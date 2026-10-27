package strategyagent

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrLeaseLost = errors.New("admissions: task lease lost")

// ClaimedTask is constructed only by the task executor after an atomic claim.
// The token is excluded from JSON, model input and framework Trace snapshots.
type ClaimedTask struct {
	Kind         string `json:"kind"`
	TaskID       int64  `json:"taskId"`
	AttemptToken string `json:"-"`
}

type HistoryMessage struct {
	Seq     int64  `json:"seq"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

type TaskMaterial struct {
	Claim                ClaimedTask      `json:"-"`
	ConversationID       int64            `json:"conversationId"`
	StrategyID           int64            `json:"strategyId"`
	SchoolID             int64            `json:"schoolId"`
	RequirementsRevision int64            `json:"requirementsRevision"`
	MessageSeq           int64            `json:"messageSeq"`
	History              []HistoryMessage `json:"history"`
	Draft                Draft            `json:"draft"`
	Snapshot             Snapshot         `json:"snapshot"`
	BaseRequirementsHash string           `json:"-"`
}

type Reply struct {
	TaskID               int64  `json:"taskId"`
	Outcome              string `json:"outcome"`
	Reply                string `json:"reply"`
	RequirementsRevision int64  `json:"requirementsRevision"`
	ProcessedMessageSeq  int64  `json:"processedMessageSeq"`
	ArtifactID           int64  `json:"artifactId,omitempty"`
}

// TaskRepository owns business reliability and transactions. A Runtime Run
// does not claim tasks or persist checkpoints. Every mutating method must CAS
// the same claim and an unexpired lease; a late attempt may never commit.
type TaskRepository interface {
	Load(context.Context, ClaimedTask) (TaskMaterial, error)
	Commit(context.Context, TaskMaterial, Draft, *Artifact, TurnResult) (Reply, error)
	Claim(context.Context, string) (ClaimedTask, bool, error)
	Renew(context.Context, ClaimedTask) error
	Fail(context.Context, ClaimedTask, error) error
	Reap(context.Context) error
}

// RunTask executes one claimed task through a supplied Agent callback. Lease
// renewal and error finalization stay outside the Agent's Prepare/Tool/Finish.
func RunTask(ctx context.Context, repository TaskRepository, claim ClaimedTask, execute func(context.Context, ClaimedTask) error, renewEvery time.Duration) error {
	if ctx == nil || repository == nil || execute == nil || renewEvery <= 0 {
		return fmt.Errorf("invalid task runner arguments")
	}
	working, cancel := context.WithCancel(ctx)
	defer cancel()
	renewed := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(renewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-working.Done():
				renewed <- nil
				return
			case <-ticker.C:
				if err := repository.Renew(working, claim); err != nil {
					cancel()
					renewed <- err
					return
				}
			}
		}
	}()
	err := executeTask(working, claim, execute)
	// A callback may finish normally after its deadline. Preserve cancellation
	// rather than treating its return as proof of a committed business result.
	err = errors.Join(err, working.Err())
	cancel()
	if renewErr := <-renewed; renewErr != nil {
		err = errors.Join(err, renewErr)
	}
	if err != nil {
		// Finalization uses a fresh bounded context; cancellation of the model
		// request must not leave a currently owned task permanently running.
		finalCtx, finish := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer finish()
		if failErr := repository.Fail(finalCtx, claim, err); failErr != nil {
			return errors.Join(err, failErr)
		}
	}
	return err
}

func executeTask(ctx context.Context, claim ClaimedTask, execute func(context.Context, ClaimedTask) error) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("admissions task callback panicked")
		}
	}()
	return execute(ctx, claim)
}
