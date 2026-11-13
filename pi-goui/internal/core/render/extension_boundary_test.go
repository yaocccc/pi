package render

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Core views consume injected policy/results rather than recognize extension
// names. Assembly lives outside this package; no compatibility routes remain.
func TestCoreToolViewsDoNotRecognizeThirdPartyNames(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{
		"worker": true, "ask_question": true, "update_plan": true, "obs_recall": true,
		"web_search": true, "fetch_content": true, "source_check": true,
		"get_search_content": true, "web_enable": true,
	}
	for _, entry := range entries {
		path := entry.Name()
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && names[value] {
				t.Errorf("%s recognizes extension %q instead of using adapter policy", fset.Position(literal.Pos()), value)
			}
			return true
		})
	}
}
