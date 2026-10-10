package lubanmq

import (
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	luban "github.com/lubanAgent/luban/sdk"
)

type testMessage struct {
	ID string
}

func TestHandlerPassesTypedMessageAndReturnsRuntimeError(t *testing.T) {
	rootErr := errors.New("business failed")
	rt, err := luban.New(luban.Options{Config: luban.Config{Agents: map[string]luban.AgentConfig{
		"mq": {Flow: []luban.NodeConfig{{Func: "Handle"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Register(map[string]any{
		"Handle": func(_ *gin.Context, message testMessage) error {
			if message.ID != "message-1" {
				t.Errorf("message = %+v", message)
			}
			return rootErr
		},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	handler := Handler[testMessage](rt, "mq")
	request, _ := http.NewRequest(http.MethodPost, "/", nil)
	err = handler(&gin.Context{Request: request}, testMessage{ID: "message-1"})
	if !errors.Is(err, rootErr) {
		t.Fatalf("handler error = %v, want original error chain", err)
	}
}
