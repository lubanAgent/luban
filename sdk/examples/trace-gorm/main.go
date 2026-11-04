// Run from the repository root:
//
//	go run ./sdk/examples/trace-gorm > /tmp/luban-trace.ndjson
//
// This example uses a fake model and a temporary SQLite file. It reads no
// company configuration, calls no real model, and never touches a remote DB.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
	"github.com/lubanAgent/luban/sdk/tracegorm"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type result struct {
	Summary string `json:"summary"`
}

type fakeModel struct{}

func (fakeModel) Name() string { return "offline" }
func (fakeModel) Invoke(*gin.Context, luban.ModelRequest) (luban.ModelResponse, error) {
	return luban.ModelResponse{ToolCalls: []luban.ToolCall{{
		ID: "output-1", Name: "__runtime_output", Arguments: `{"summary":"本地示例：准备材料、模型生成、结构化结果和 Trace 落库。"} `,
	}}, FinishReason: "tool_calls", Usage: &luban.ModelUsage{InputTokens: 10, OutputTokens: 5}}, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := os.MkdirTemp("", "luban-trace-example-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "trace.sqlite")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	// Only this offline example creates its disposable schema explicitly.
	// Production uses sql/trace/001_create_tables.mysql.sql via the host's
	// database change process; tracegorm.New never creates schema.
	schema, err := os.ReadFile("sdk/tracegorm/testdata/schema.sqlite.sql")
	if err != nil {
		return fmt.Errorf("run this example from the Luban repository root: %w", err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		return err
	}
	writer, err := tracegorm.New(context.Background(), db, tracegorm.Options{
		Service: "example",
		Metadata: func(ctx *gin.Context) tracegorm.Metadata {
			return tracegorm.Metadata{BusinessType: "demo", BusinessID: ctx.GetString("businessID")}
		},
	})
	if err != nil {
		return err
	}
	// Also drain if a later initialization step fails.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := writer.Close(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "Trace drain:", err)
		}
	}()
	rt, err := luban.New(luban.Options{
		Config: luban.Config{Agents: map[string]luban.AgentConfig{"summary": {Flow: []luban.NodeConfig{
			{Func: "Prepare", Description: "准备文本"},
			{ModelName: "offline", SystemPrompt: "总结输入，提交结构化结果。"},
		}}}},
		Models: map[string]luban.ModelInvoker{"offline": fakeModel{}}, TraceWriter: writer,
	})
	if err != nil {
		return err
	}
	if err := rt.Register(map[string]any{
		"Prepare": func(_ *gin.Context, text string) luban.Request {
			return luban.Request{
				Messages: []luban.Message{{Role: luban.RoleUser,
					Content: []luban.Part{{Type: luban.PartText, Text: text}}}},
				Output: luban.Output[result](),
			}
		},
	}, nil, nil); err != nil {
		return err
	}
	ctx := &gin.Context{}
	ctx.Set("businessID", "example-1")
	output, err := rt.Run(ctx, "summary", "验证 SDK 生成的 Trace 能够入库并导出。")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "runId=%s result=%+v\n", luban.RunID(ctx), output)
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := writer.Close(shutdown); err != nil {
		return err
	}
	// Parameterized export: each line is the original stored event JSON.
	var rows []struct{ EventJSON string }
	if err := db.Table(tracegorm.SpanTable(luban.RunID(ctx))).Select("event_json").
		Where("run_id = ?", luban.RunID(ctx)).Order("started_at, span_id").Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(os.Stdout, row.EventJSON); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "Trace stats: %+v\n", writer.Stats())
	return nil
}
