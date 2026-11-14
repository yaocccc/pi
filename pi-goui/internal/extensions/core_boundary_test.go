package extensions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInternalDirectoryLayout(t *testing.T) {
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatal("internal must contain only the core, extensions and runtime directories")
	}
	for i, name := range []string{"core", "extensions", "runtime"} {
		if !entries[i].IsDir() || entries[i].Name() != name {
			t.Fatalf("unexpected internal entry: %s", entries[i].Name())
		}
	}
}

// All production core packages use one composition root. Merely adding a new
// adapter file must not bypass this check; tests can exercise public adapters.
func TestCoreExtensionBoundary(t *testing.T) {
	const entry = "core/ui/extensions.go"
	const root = "pi-goui/internal/extensions"
	imports := 0
	names := map[string]bool{"worker": true, "ask_question": true, "update_plan": true, "obs_recall": true, "web_search": true, "fetch_content": true, "get_search_content": true, "source_check": true, "web_enable": true}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == filepath.Join("..", "extensions") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel("..", path)
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			value, _ := strconv.Unquote(imp.Path.Value)
			if value == root && rel == entry {
				imports++
				name := "extensions"
				if imp.Name != nil {
					name = imp.Name.Name
				}
				if name == "." {
					t.Error("composition import must not expose concrete names")
				}
				ast.Inspect(file, func(node ast.Node) bool {
					selector, ok := node.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					ident, ok := selector.X.(*ast.Ident)
					if ok && ident.Name == name && selector.Sel.Name != "New" {
						t.Errorf("%s accesses concrete extension export %s: consume only New() interface slice", rel, selector.Sel.Name)
					}
					return true
				})
			} else if value == root || strings.HasPrefix(value, root+"/") {
				t.Errorf("%s imports %s: only %s may import the root package", rel, value, entry)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, _ := strconv.Unquote(lit.Value)
			// Existing palette slot is visual compatibility data, not dispatch.
			if rel == "core/render/frame/presets.go" && value == "worker" {
				return true
			}
			if names[value] || strings.HasPrefix(value, "pi-goui:worker") || strings.HasPrefix(value, "pi-goui:ask-question") || strings.HasPrefix(value, "pi-goui:codex-fast") || strings.HasPrefix(value, "sol-pi-") || value == "web-search-content-ready" {
				t.Errorf("%s recognizes extension protocol %q", fset.Position(lit.Pos()), value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if imports != 1 {
		t.Fatalf("want one root import in %s, got %d", entry, imports)
	}
}

func TestExtensionImplementationsDoNotImportHostOrAssembly(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			value, _ := strconv.Unquote(imp.Path.Value)
			if value == "pi-goui/internal/extensions" || value == "pi-goui/internal/core/ui" || strings.HasPrefix(value, "pi-goui/internal/core/ui/") {
				t.Errorf("%s imports composition root or UI host %s", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
