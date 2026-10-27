package worker

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents/admissions"
	"github.com/lubanAgent/luban/models/strategyrepo"
	"github.com/lubanAgent/luban/router"
	"github.com/lubanAgent/luban/server"
)

// Mount is the business-only assembly for asynchronous admissions tasks.
// Merely loading the Agent or injecting a database never starts this worker.
func Mount(ctx context.Context, engine *gin.Engine, app *server.App) error {
	db, ok := app.Database("admissions")
	if !ok {
		return fmt.Errorf("admissions background Agent requires database admissions")
	}
	repository, err := strategyrepo.NewGormRepository(db)
	if err != nil {
		return err
	}
	if err := repository.ValidateSchema(); err != nil {
		return err
	}
	taskWorker, err := New(app.Runtime(), repository)
	if err != nil {
		return err
	}
	router.RegisterSubmission(engine, "/admissions/mis/strategy/conversation/message",
		admissions.SubmitTo(repository), app.Render)
	taskWorker.Start(ctx, app.ReportError)
	app.OnShutdown("admissionsWorker", taskWorker.Close)
	return nil
}
