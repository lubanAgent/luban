package agents_test

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/agents"
	"github.com/lubanAgent/luban/models"
	"github.com/lubanAgent/luban/server"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func taskAssets(t *testing.T, enabled bool) fstest.MapFS {
	t.Helper()
	config := "http:\n  excludeAgents: [admissions]\nllm:\n  admissions:\n    - baseUrl: https://fake.invalid/v1\n      apiKey: fake-key\n"
	if enabled {
		config += "backgroundAgents: [admissions]\n"
	}
	assets := fstest.MapFS{"conf/luban.yaml": {Data: []byte(config)}}
	source := os.DirFS("..")
	if err := fs.WalkDir(source, "agents", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (!strings.HasSuffix(name, "/agent.json") && !strings.HasSuffix(name, "/SKILL.md")) {
			return nil
		}
		raw, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		assets[name] = &fstest.MapFile{Data: raw}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return assets
}

func TestBusinessWorkerIsOptInAndStopsBeforeDatabaseCloses(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.Strategy{}, &models.StrategyConversation{}, &models.StrategyMessage{}, &models.StrategyMessageTask{}, &models.StrategyGenTask{}, &models.AgentDraft{}, &models.AgentArtifact{}, &models.ConfigItem{}); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		var shutdown func()
		app, err := server.New(server.Options{Assets: taskAssets(t, enabled), Databases: map[string]*gorm.DB{"admissions": db}, Stdout: io.Discard, Stderr: io.Discard,
			RegisterShutdown: func(name string, stop func()) {
				if name != "admissionsWorker" {
					t.Error("unexpected callback")
				}
				shutdown = stop
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		engine := gin.New()
		if err := app.RegisterHTTP(engine); err != nil {
			t.Fatal(err)
		}
		if err := agents.Mount(context.Background(), engine, app); err != nil {
			t.Fatal(err)
		}
		routes := map[string]bool{}
		for _, route := range engine.Routes() {
			routes[route.Path] = true
		}
		if routes["/luban/ai/agent/admissions"] {
			t.Fatal("trusted task Agent exposed synchronously")
		}
		if routes["/admissions/mis/strategy/conversation/message"] != enabled || (shutdown != nil) != enabled {
			t.Fatal("business routes/worker must be explicitly enabled")
		}
		if enabled {
			// Stop before creating tasks, so the test cannot call a real gateway.
			done := make(chan struct{})
			go func() { shutdown(); close(done) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("worker shutdown blocked")
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/admissions/mis/strategy/conversation/message", strings.NewReader(`{"action":"message","schoolId":16,"content":"按总分推荐学校","requestId":"mount-test"}`))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(recorder, request)
			var response struct {
				ErrNo int `json:"errNo"`
				Data  struct {
					TaskID int64 `json:"taskId"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != 200 || response.ErrNo != 0 || response.Data.TaskID <= 0 {
				t.Fatalf("submission=%s", recorder.Body.String())
			}
		}
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
		if shutdown != nil {
			shutdown()
		}
		if err := sqlDB.Ping(); err != nil {
			t.Fatal("injected DB was closed")
		}
	}
}

func TestEnablingBusinessWorkerWithoutDatabaseFails(t *testing.T) {
	app, err := server.New(server.Options{Assets: taskAssets(t, true), Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := agents.Mount(context.Background(), gin.New(), app); err == nil || !strings.Contains(err.Error(), "requires database admissions") {
		t.Fatalf("missing DB: %v", err)
	}
}
