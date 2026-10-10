package lubancron

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

func TestJobContinuesAfterItemErrorAndIsolatesContext(t *testing.T) {
	rootErr := errors.New("bad lecture")
	processed := make([]string, 0, 3)
	rt, err := luban.New(luban.Options{Config: luban.Config{Agents: map[string]luban.AgentConfig{
		"cron": {Flow: []luban.NodeConfig{{Func: "Handle"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"Handle": func(ctx *gin.Context, lesson string) (string, error) {
			if _, exists := ctx.Get("previous-item"); exists {
				t.Error("Gin Context data from a prior Cron item leaked")
			}
			ctx.Set("previous-item", lesson)
			processed = append(processed, lesson)
			if lesson == "bad" {
				return "", rootErr
			}
			return lesson, nil
		},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, "/", nil)
	ctx := &gin.Context{Request: request}
	ctx.Set("job", "stable")

	job := Job[string](rt, "cron", func(*gin.Context, ...string) ([]string, error) {
		return []string{"first", "bad", "last"}, nil
	})
	err = job(ctx)
	if !errors.Is(err, rootErr) {
		t.Fatalf("Job error = %v, want original item error", err)
	}
	if strings.Count(err.Error(), "item[1]") != 1 {
		t.Fatalf("Job error lacks item index: %v", err)
	}
	if strings.Join(processed, ",") != "first,bad,last" {
		t.Fatalf("processed items = %v; later items should continue after an error", processed)
	}
	if _, exists := ctx.Get("previous-item"); exists {
		t.Fatal("item Context mutation leaked to the Job Context")
	}
	if value, exists := ctx.Get("job"); !exists || value != "stable" {
		t.Fatal("base Job Context values were not copied into each item")
	}
}

func TestJobEmptyAndLoaderFailure(t *testing.T) {
	rt, err := luban.New(luban.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, "/", nil)
	ctx := &gin.Context{Request: request}

	empty := Job[string](rt, "cron", func(*gin.Context, ...string) ([]string, error) {
		return nil, nil
	})
	if err := empty(ctx); err != nil {
		t.Fatalf("empty loader result should succeed: %v", err)
	}

	rootErr := errors.New("loader failed")
	failed := Job[string](rt, "cron", func(*gin.Context, ...string) ([]string, error) {
		return nil, rootErr
	})
	if err := failed(ctx); !errors.Is(err, rootErr) {
		t.Fatalf("loader error = %v, want original error chain", err)
	}
}
