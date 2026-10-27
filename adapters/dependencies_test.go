package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.yukework.com/pkg/golib/v2/env"
	"git.yukework.com/pkg/golib/v2/zlog"
	"github.com/gin-gonic/gin"
	"github.com/lubanAgent/luban/conf"
	luban "github.com/lubanAgent/luban/sdk"
)

type logBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}
func (b *logBuffer) Reset()         { b.mu.Lock(); defer b.mu.Unlock(); b.data.Reset() }
func (b *logBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.data.String() }

var companyLogs logBuffer

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "luban-company-log-")
	if err != nil {
		panic(err)
	}
	env.SetRootPath(root)
	zlog.RegisterTestLogWriter(func(_, _ string) io.Writer { return &companyLogs })
	zlog.InitLog(zlog.LogConfig{Level: "debug", Buffer: zlog.Buffer{Switch: "false"}})
	code := m.Run()
	zlog.CloseLogger()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func TestCompanyLoggerAndTraceUseZlog(t *testing.T) {
	companyLogs.Reset()
	logger := companyLogger{}
	logger.Debugf(nil, "company debug %d", 1)
	logger.Infof(nil, "company info %d", 2)
	logger.Warnf(nil, "company warn %d", 3)
	logger.Errorf(nil, "company error %d", 4)
	companyTraceWriter{}.WriteTrace(nil, luban.TraceEvent{RunID: "company-trace-test"})
	for _, expected := range []string{"company debug 1", "company info 2", "company warn 3", "company error 4", `"runId":"company-trace-test"`, `"traceEvent":`} {
		if !strings.Contains(companyLogs.String(), expected) {
			t.Errorf("golib log missing %q", expected)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func reply(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func modelContext(t *testing.T) *gin.Context {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/test", nil)
	return ctx
}

func TestCompanyHTTPPreservesSDKProtocolsToolsMediaAndUsage(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			client := newModelHTTP(conf.ModelHTTP{})
			client.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				key := r.Header.Get("Authorization")
				path := "/gateway/v1/chat/completions"
				if protocol == "anthropic" {
					key = r.Header.Get("x-api-key")
					path = "/gateway/v1/messages"
				}
				if r.URL.String() != "https://gateway.invalid"+path || !strings.HasSuffix(key, "fake-key") {
					t.Error("gateway routing/auth lost")
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if string(body["model"]) != `"any-real-model"` || !bytes.Contains(body["messages"], []byte("image")) || !bytes.Contains(body["tools"], []byte("query")) {
					t.Fatalf("protocol payload lost: %s", body)
				}
				if protocol == "anthropic" {
					return reply(`{"type":"message","content":[{"type":"tool_use","id":"query-1","name":"query","input":{"id":1}}],"stop_reason":"tool_use","usage":{"input_tokens":123,"output_tokens":45}}`), nil
				}
				return reply(`{"choices":[{"message":{"tool_calls":[{"id":"query-1","type":"function","function":{"name":"query","arguments":"{\"id\":1}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":123,"completion_tokens":45}}`), nil
			})
			model, err := luban.NewProtocolInvoker(luban.ModelConnection{BaseURL: "https://gateway.invalid/gateway/v1", APIKey: "fake-key", Protocol: protocol}, client)
			if err != nil {
				t.Fatal(err)
			}
			response, err := model.Invoke(modelContext(t), luban.ModelRequest{ModelName: "any-real-model", Messages: []luban.ModelMessage{{Role: luban.ModelRoleUser, Content: []luban.ModelPart{{Type: luban.PartText, Text: "hello"}, {Type: luban.PartImage, URL: "https://image.invalid/test.png"}}}}, Tools: []luban.ToolDefinition{{Name: "query", Parameters: json.RawMessage(`{"type":"object"}`)}}})
			if err != nil || len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "query" || response.Usage == nil || response.Usage.InputTokens != 123 || response.Usage.OutputTokens != 45 {
				t.Fatalf("response=%+v err=%v", response, err)
			}
			if strings.Contains(companyLogs.String(), "fake-key") {
				t.Fatal("model key leaked")
			}
		})
	}
}

func TestCompanyHTTPConcurrentConnectionsKeepTheirOwnCredentials(t *testing.T) {
	client := newModelHTTP(conf.ModelHTTP{})
	client.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+r.URL.Host {
			t.Errorf("credentials crossed projects")
		}
		return reply(`{"choices":[{"message":{"content":"ok"}}]}`), nil
	})
	var group sync.WaitGroup
	for _, host := range []string{"first.invalid", "second.invalid"} {
		model, err := luban.NewProtocolInvoker(luban.ModelConnection{BaseURL: "https://" + host + "/v1", APIKey: host}, client)
		if err != nil {
			t.Fatal(err)
		}
		ctx := modelContext(t)
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < 10; i++ {
				if _, err := model.Invoke(ctx, luban.ModelRequest{ModelName: "same-model"}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	group.Wait()
}

func TestCompanyHTTPRetriesAndCancellation(t *testing.T) {
	client := newModelHTTP(conf.ModelHTTP{Retry: 1, Timeout: time.Second})
	var calls atomic.Int32
	client.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("temporary transport failure")
		}
		return reply(`{"choices":[{"message":{"content":"ok"}}]}`), nil
	})
	model, err := luban.NewProtocolInvoker(luban.ModelConnection{BaseURL: "https://retry.invalid/v1", APIKey: "secret"}, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.Invoke(modelContext(t), luban.ModelRequest{ModelName: "m"}); err != nil || calls.Load() != 2 {
		t.Fatalf("retry calls=%d err=%v", calls.Load(), err)
	}
	entered := make(chan struct{})
	client.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	client.settings.Retry = 0
	ctx := modelContext(t)
	parent, cancel := context.WithCancel(ctx.Request.Context())
	defer cancel()
	ctx.Request = ctx.Request.WithContext(parent)
	done := make(chan error, 1)
	go func() { _, err := model.Invoke(ctx, luban.ModelRequest{ModelName: "m"}); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("transport not entered")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run cancellation did not reach company HTTP")
	}
}

func TestCompanyHTTPTimeoutStillCancelsTransport(t *testing.T) {
	client := newModelHTTP(conf.ModelHTTP{Timeout: 25 * time.Millisecond})
	client.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	model, err := luban.NewProtocolInvoker(luban.ModelConnection{BaseURL: "https://timeout.invalid/v1", APIKey: "secret"}, client)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := model.Invoke(modelContext(t), luban.ModelRequest{ModelName: "m"}); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("configured HTTP timeout did not reach transport")
	}
}
