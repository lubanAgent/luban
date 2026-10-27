package eino

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type recursiveCondition struct {
	Op       string               `json:"op"`
	Children []recursiveCondition `json:"children,omitempty"`
	Child    *recursiveCondition  `json:"child,omitempty"`
}

func TestRecursiveToolSchemaKeepsLocalDefinitions(t *testing.T) {
	info, err := toolInfoFor("edit", "Edit a recursive condition", reflect.TypeFor[recursiveCondition]())
	if err != nil {
		t.Fatal(err)
	}
	schema, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"$defs"`) || !strings.Contains(string(raw), `"$ref":"#/$defs/recursiveCondition"`) || len(raw) > 10000 {
		t.Fatalf("recursive schema = %s", raw)
	}
	tool, err := newBusinessEinoTool(Tool{Name: "edit", Description: "Edit", Input: recursiveCondition{}, Invoke: func(_ *gin.Context, input any) (any, error) { return input, nil }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	stdCtx, _ := WithGinContext(context.Background(), ctx)
	output, err := tool.InvokableRun(stdCtx, `{"op":"and","children":[{"op":"eq"}]}`)
	if err != nil || !strings.Contains(output, `"op":"eq"`) {
		t.Fatalf("recursive decode: %s, %v", output, err)
	}
}
