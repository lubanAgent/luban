package adapters

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Adapters may know company infrastructure, never an Agent's domain or model.
func TestAdaptersHaveNoBusinessImportsOrProductionTestFS(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			for _, forbidden := range []string{"/agents", "/models/strategyrepo", "/router", "testing/fstest"} {
				if strings.Contains(imported.Path.Value, forbidden) {
					t.Errorf("%s imports %s", file.Name(), imported.Path.Value)
				}
			}
		}
	}
}
