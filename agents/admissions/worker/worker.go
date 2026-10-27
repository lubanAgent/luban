// Package worker runs admissions tasks with the shared Runtime.
package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents/admissions"
	strategyagent "github.com/lubanAgent/luban/agents/admissions/strategy"
	"github.com/lubanAgent/luban/models/strategyrepo"
	luban "github.com/lubanAgent/luban/sdk"
)

// Worker owns the domain task loop. Scheduling, leases and retries are
// business responsibilities, separate from the Runtime's one-Run execution.
type Worker struct {
	repository *strategyrepo.GormRepository
	runtime    *luban.Runtime
	cancel     context.CancelFunc
	wait       sync.WaitGroup
}

func New(rt *luban.Runtime, repository *strategyrepo.GormRepository) (*Worker, error) {
	if rt == nil || repository == nil {
		return nil, fmt.Errorf("admissions Runtime and repository are required")
	}
	return &Worker{runtime: rt, repository: repository}, nil
}

// Start uses durable pending rows as the source of work. This ticker discovers
// work and expires leases; it is not a generic scheduling feature in Luban.
func (a *Worker) Start(parent context.Context, report func(error)) {
	if report == nil {
		report = func(error) {}
	}
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	slots := make(chan struct{}, 2)
	a.wait.Add(1)
	go func() {
		defer a.wait.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if err := a.repository.Reap(ctx); err != nil {
				report(err)
				continue
			}
			for _, kind := range []string{"chat", "generate"} {
				// Reserve capacity before claiming so a full worker pool leaves
				// pending tasks visible to another process or the next poll.
				select {
				case slots <- struct{}{}:
				default:
					continue
				}
				claim, found, err := a.repository.Claim(ctx, kind)
				if err != nil {
					<-slots
					report(err)
					continue
				}
				if !found {
					<-slots
					continue
				}
				a.wait.Add(1)
				go func(claim strategyagent.ClaimedTask) {
					defer a.wait.Done()
					defer func() { <-slots }()
					if err := a.Execute(ctx, claim); err != nil {
						report(err)
					}
				}(claim)
			}
		}
	}()
}

// Execute is the authorized task entry: it runs the complete Agent once.
// This worker reads the database directly, so it does not pretend to consume
// an MQ message. Prepare, tools and Finish never call Runtime.Run themselves.
func (a *Worker) Execute(parent context.Context, claim strategyagent.ClaimedTask) error {
	timeout := 2 * time.Minute
	if claim.Kind == "generate" {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return strategyagent.RunTask(ctx, a.repository, claim, func(current context.Context, task strategyagent.ClaimedTask) error {
		// The task has its own Gin Keys and cancellation; no fake HTTP request.
		ginCtx := &gin.Context{}
		if err := luban.BindContext(ginCtx, current); err != nil {
			return err
		}
		admissions.BindTaskRepository(ginCtx, a.repository)
		_, err := a.runtime.Run(ginCtx, "admissions", task)
		return err
	}, 20*time.Second)
}

func (a *Worker) Close() {
	if a.cancel != nil {
		a.cancel()
	}
	a.wait.Wait()
}
