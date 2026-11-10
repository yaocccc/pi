package render

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"pi-goui/internal/config"
)

// visualWrap never splits a grapheme or emits an over-wide terminal row. A
// double-width grapheme cannot fit a one-cell viewport: show one replacement
// glyph there, retaining the original source in the session projection.
func visualWrap(s string, width int) []string {
	width = max(1, width)
	rows := strings.Split(ansi.Hardwrap(s, width, true), "\n")
	for i, row := range rows {
		if ansi.StringWidth(row) <= width {
			continue
		}
		var out strings.Builder
		g := uniseg.NewGraphemes(ansi.Strip(row))
		for g.Next() {
			part := g.Str()
			if g.Width() > width {
				part = "�"
			}
			out.WriteString(part)
		}
		rows[i] = out.String()
	}
	return rows
}
func visualDim(theme config.Theme) string {
	if theme.Dim != "" {
		return theme.Dim
	}
	return theme.Muted
}
func cleanCode(source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	var out strings.Builder
	column := 0
	g := uniseg.NewGraphemes(source)
	for g.Next() {
		s := g.Str()
		switch s {
		case "\t":
			n := 4 - column%4
			out.WriteString(strings.Repeat(" ", n))
			column += n
		case "\n":
			out.WriteByte('\n')
			column = 0
		default:
			out.WriteString(s)
			column += g.Width()
		}
	}
	return Clean(out.String())
}
func visualTokenColor(t chroma.TokenType, theme config.Theme) string {
	switch {
	case t.InCategory(chroma.Comment):
		return visualDim(theme)
	case t.InCategory(chroma.Keyword):
		return theme.Warning
	case t.InSubCategory(chroma.LiteralString):
		return theme.Success
	case t.InSubCategory(chroma.LiteralNumber):
		return theme.Accent
	case t == chroma.NameFunction || t == chroma.NameClass || t == chroma.NameBuiltin:
		return theme.Accent
	default:
		return theme.Text
	}
}
func highlightedLines(source, path, language string, theme config.Theme) []string {
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
		parts := strings.Split(token.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				rows = append(rows, "")
			}
			rows[len(rows)-1] += paint(part, visualTokenColor(token.Type, theme))
		}
	}
	return rows
}

// codeRows uses source line numbers, not terminal rows. Wrapped continuations
// retain an empty gutter; replacement previews pass zero for relative numbering.
func codeRows(source, path, language string, start, width int, theme config.Theme) []string {
	source = cleanCode(source)
	if source == "" {
		return visualWrap(paint("(empty)", visualDim(theme)), width)
	}
	source = strings.TrimSuffix(source, "\n")
	rows := highlightedLines(source, path, language, theme)
	// Lexer-added trailing newlines are not part of the saved source.
	expected := strings.Count(source, "\n") + 1
	if len(rows) > expected {
		rows = rows[:expected]
	}
	if len(rows) == 0 {
		rows = []string{""}
	}
	if start < 1 {
		start = 1
	}
	digits := len(strconv.Itoa(start + len(rows) - 1))
	gutter := digits + 3
	var out []string
	for i, row := range rows {
		if width < gutter+3 {
			out = append(out, visualWrap(row, width)...)
			continue
		}
		wrapped := visualWrap(row, width-gutter)
		for j, part := range wrapped {
			prefix := strings.Repeat(" ", digits) + " │ "
			if j == 0 {
				prefix = fmt.Sprintf("%*d │ ", digits, start+i)
			}
			out = append(out, paint(prefix, visualDim(theme))+part)
		}
	}
	return out
}
func diffCounts(diff string) (added, removed int) {
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff ") {
			inHunk = false
		}
		if strings.HasPrefix(line, "@@") {
			inHunk = true
		}
		if !inHunk && (strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- ")) {
			continue
		}
		if strings.HasPrefix(line, "+") {
			added++
		}
		if strings.HasPrefix(line, "-") {
			removed++
		}
	}
	return
}
func diffRows(diff string, width int, theme config.Theme) []string {
	var out []string
	inHunk := false
	for _, line := range strings.Split(cleanCode(diff), "\n") {
		if strings.HasPrefix(line, "diff ") {
			inHunk = false
		}
		if strings.HasPrefix(line, "@@") {
			inHunk = true
		}
		color := theme.Text
		switch {
		case (!inHunk && (strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "))) || strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "diff "):
			color = theme.Accent
		case strings.HasPrefix(line, "+"):
			color = theme.Success
		case strings.HasPrefix(line, "-"):
			color = theme.Error
		case strings.TrimSpace(line) == "...":
			color = visualDim(theme)
		}
		out = append(out, visualWrap(paint(line, color), width)...)
	}
	return out
}
