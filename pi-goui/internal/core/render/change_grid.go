package render

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
)

type changeLine struct {
	kind     byte
	text     string
	styled   string
	old, new int
}

const changeContentWidth = 100

var changeHunk = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
var changeNumber = regexp.MustCompile(`^(\d+) (.*)$`)

// Recorded Pi diffs carry explicit line numbers; unified diffs carry hunk
// coordinates. Unnumbered replacement previews deliberately have no numbers.
func parseChangeLines(diff string, recorded bool) []changeLine {
	var out []changeLine
	old, next := 0, 0
	inHunk := false
	for _, s := range strings.Split(strings.TrimSuffix(cleanCode(diff), "\n"), "\n") {
		if strings.HasPrefix(s, "diff ") {
			inHunk = false
			old, next = 0, 0
		}
		if m := changeHunk.FindStringSubmatch(s); m != nil {
			old, _ = strconv.Atoi(m[1])
			next, _ = strconv.Atoi(m[2])
			inHunk = true
			out = append(out, changeLine{kind: '@', text: s})
			continue
		}
		if s == "" || (recorded && !inHunk && (strings.HasPrefix(s, "--- ") || strings.HasPrefix(s, "+++ "))) || !strings.ContainsRune(" +-", rune(s[0])) {
			out = append(out, changeLine{kind: '@', text: s})
			continue
		}
		c := changeLine{kind: s[0], text: s[1:]}
		if !inHunk {
			if m := changeNumber.FindStringSubmatch(c.text); recorded && m != nil {
				n, _ := strconv.Atoi(m[1])
				c.text = m[2]
				if c.kind != '+' {
					c.old = n
				}
				if c.kind != '-' {
					c.new = n
				}
			}
		} else {
			if c.kind != '+' {
				c.old = old
				old++
			}
			if c.kind != '-' {
				c.new = next
				next++
			}
		}
		out = append(out, c)
	}
	return out
}

func changeCell(c *changeLine, number, width, digits int, path string, theme config.Theme) []string {
	if c == nil {
		return []string{""}
	}
	color, bar := visualDim(theme), " "
	if c.kind == '+' {
		color, bar = theme.Success, "▌"
	}
	if c.kind == '-' {
		color, bar = theme.Error, "▌"
	}
	text := c.styled
	gutter := digits + 5
	if width < gutter+3 {
		return []string{fit(paint(string(c.kind), color)+text, width)}
	}
	num := ""
	if number > 0 {
		num = strconv.Itoa(number)
	}
	return []string{paint(bar+fmt.Sprintf(" %*s", digits, num), color) + paint(" │ ", visualDim(theme)) + fit(text, width-gutter)}
}

func changeGrid(diff, path string, width int, theme config.Theme, recorded bool) []string {
	width = max(1, min(width, changeContentWidth))
	lines := parseChangeLines(diff, recorded)
	// Highlight each side in one pass, preserving multiline lexical state.
	for _, side := range []byte{'-', '+'} {
		var source []string
		var indices []int
		for i, c := range lines {
			if c.kind == side || c.kind == ' ' {
				source = append(source, c.text)
				indices = append(indices, i)
			}
		}
		styled := highlightedLines(strings.Join(source, "\n"), path, "", theme)
		for i, index := range indices {
			lines[index].styled = lines[index].text
			if i < len(styled) {
				lines[index].styled = styled[i]
			}
		}
	}
	digits := 1
	for _, c := range lines {
		digits = max(digits, len(strconv.Itoa(max(c.old, c.new))))
	}
	split := width >= 72
	mode := "unified"
	if split {
		mode = "split"
	}
	added, removed := 0, 0
	for _, c := range lines {
		if c.kind == '+' {
			added++
		}
		if c.kind == '-' {
			removed++
		}
	}
	out := []string{fit(paint(fmt.Sprintf("↳ diff +%d -%d %s", added, removed, mode), visualDim(theme)), width)}
	out = append(out, paint(strings.Repeat("─", max(1, width)), visualDim(theme)))
	left := (width - 3) / 2
	right := width - left - 3
	join := func(a, b string) string {
		return a + strings.Repeat(" ", max(0, left-ansi.StringWidth(a))) + paint(" │ ", visualDim(theme)) + b
	}
	if split {
		out = append(out, join(paint("old", visualDim(theme)), paint("new", visualDim(theme))))
	}
	pair := func(a, b *changeLine) {
		an, bn := 0, 0
		if a != nil {
			an = a.old
		}
		if b != nil {
			bn = b.new
		}
		ar, br := changeCell(a, an, left, digits, path, theme), changeCell(b, bn, right, digits, path, theme)
		for i := 0; i < max(len(ar), len(br)); i++ {
			x, y := "", ""
			if i < len(ar) {
				x = ar[i]
			}
			if i < len(br) {
				y = br[i]
			}
			out = append(out, join(x, y))
		}
	}
	for i := 0; i < len(lines); {
		c := lines[i]
		if c.kind == '@' {
			out = append(out, fit(paint(c.text, visualDim(theme)), width))
			i++
			continue
		}
		if !split {
			n := c.new
			if c.kind == '-' {
				n = c.old
			}
			out = append(out, changeCell(&c, n, width, digits, path, theme)...)
			i++
			continue
		}
		if c.kind == ' ' {
			pair(&c, &c)
			i++
			continue
		}
		var before, after []changeLine
		for i < len(lines) && lines[i].kind == '-' {
			before = append(before, lines[i])
			i++
		}
		for i < len(lines) && lines[i].kind == '+' {
			after = append(after, lines[i])
			i++
		}
		for j := 0; j < max(len(before), len(after)); j++ {
			var a, b *changeLine
			if j < len(before) {
				a = &before[j]
			}
			if j < len(after) {
				b = &after[j]
			}
			pair(a, b)
		}
	}
	return out
}

func writtenGrid(content, path string, width int, theme config.Theme) []string {
	width = max(1, min(width, changeContentWidth))
	if content == "" {
		return []string{fit(paint("(empty content)", visualDim(theme)), width)}
	}
	lines := strings.Split(strings.TrimSuffix(cleanCode(content), "\n"), "\n")
	out := []string{paint(strings.Repeat("─", max(1, width)), visualDim(theme))}
	styled := highlightedLines(strings.Join(lines, "\n"), path, "", theme)
	start := 0
	if len(lines) > 10 {
		label := fmt.Sprintf("… %d earlier lines hidden", len(lines)-10)
		start = len(lines) - 10
		out = append(out, fit(paint(label, visualDim(theme)), width))
	}
	for i := start; i < len(lines); i++ {
		line := lines[i]
		c := changeLine{kind: '+', text: line, styled: line}
		if i < len(styled) {
			c.styled = styled[i]
		}
		out = append(out, changeCell(&c, i+1, width, len(strconv.Itoa(len(lines))), path, theme)...)
	}
	return out
}
