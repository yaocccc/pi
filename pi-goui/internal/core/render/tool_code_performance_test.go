package render

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"pi-goui/internal/core/config"
)

// Keep the previous concatenating implementation as a small-input oracle for
// exact ANSI, blank-line and trailing-newline behavior, not as production code.
func concatenatedHighlight(source, path, language string, theme config.Theme) []string {
	source = cleanCode(source)
	lexer := lexers.Get(language)
	if language == "" {
		lexer = lexers.Match(filepath.Base(path))
	}
	if lexer == nil || len(source) > 256<<10 {
		return strings.Split(source, "\n")
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, source)
	if err != nil {
		return strings.Split(source, "\n")
	}
	rows := []string{""}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		for i, part := range strings.Split(token.Value, "\n") {
			if i > 0 {
				rows = append(rows, "")
			}
			rows[len(rows)-1] += paint(part, visualTokenColor(token.Type, theme))
		}
	}
	return rows
}

func TestHighlightBuilderPreservesOutput(t *testing.T) {
	for _, tc := range []struct{ source, path, language string }{
		{"", "empty.go", ""},
		{"\n\n", "empty.js", ""},
		{"const x = 1;\n\n// 中文 e\u0301 👩‍💻\nconsole.log(x);\n", "code.js", ""},
		{"def f(x):\r\n\treturn x + 1\r\n", "", "python"},
		{strings.Repeat("x+=1;", 400), "code.js", ""},
		{"plain\n\ntext\n", "unknown.extension", ""},
		{strings.Repeat("a", 256<<10+1), "large.js", ""},
	} {
		got := highlightedLines(tc.source, tc.path, tc.language, testTheme())
		want := concatenatedHighlight(tc.source, tc.path, tc.language, testTheme())
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("highlight output changed for path=%q language=%q length=%d", tc.path, tc.language, len(tc.source))
		}
	}
}

func BenchmarkHighlightSingleLine(b *testing.B) {
	for _, size := range []int{2048, 8192, 32768} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			source := strings.Repeat("x+=1;", size/5)
			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			for b.Loop() {
				if len(highlightedLines(source, "code.js", "", testTheme())) != 1 {
					b.Fatal("unexpected line break")
				}
			}
		})
	}
}
