package toolrender_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAdaptersDoNotImportHostImplementationsOrIO(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			name, _ := strconv.Unquote(spec.Path.Value)
			// Only the fixed assembly may refer to the neutral host provider type.
			if path == "views.go" && name == "pi-goui/internal/core/render" {
				continue
			}
			switch name {
			case "pi-goui/internal/core/render", "pi-goui/internal/core/render/tooldetail", "pi-goui/internal/core/ui", "os", "os/exec", "net/http":
				t.Errorf("%s imports host implementation or IO %s", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
