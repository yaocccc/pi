package input

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

func TestScrollGeometryMatchesTextarea(t *testing.T) {
	cases := []string{
		"", "\n", "\n\n", "hello\n", "a  b   c", "12345678", "abcdefghijklmno",
		"界界 a界", "e\u0301\u0308e\u0301 \u0301", "♥️👩🏽‍💻🇨🇳 x", "a\t\u2003b\u00a0c",
		strings.Repeat("abc ", 30), strings.Repeat("界e\u0301👩‍💻 ", 20),
	}
	// Include mixed grapheme/word boundaries, especially widths narrower than
	// a wide rune, and exact-width trailing caret rows.
	rng := rand.New(rand.NewPCG(1, 2))
	alphabet := []rune("a界♥️👩🏽‍💻🇨🇳e\u0301 \t\n")
	for range 60 {
		var text strings.Builder
		for range 30 {
			text.WriteRune(alphabet[rng.IntN(len(alphabet))])
		}
		cases = append(cases, text.String())
	}
	for style := range 3 {
		for _, width := range []int{1, 2, 3, 8, 17, 40} {
			for i, text := range cases {
				e := New()
				if style == 1 {
					e.Area.ShowLineNumbers = true
					e.Area.Prompt = "> "
				}
				if style == 2 {
					styles := e.Area.Styles()
					styles.Focused.Base = styles.Focused.Base.Padding(1, 2)
					styles.Blurred.Base = styles.Blurred.Base.Padding(1, 2)
					e.Area.SetStyles(styles)
				}
				e.Area.DynamicHeight = true
				e.SetValue(text)
				e.Area.SetWidth(width)
				_, total, _ := e.ScrollPosition()
				if total != e.Area.Height() {
					t.Fatalf("case=%d style=%d width=%d contentWidth=%d total=%d native=%d text=%q", i, style, width, e.Area.Width(), total, e.Area.Height(), e.Value())
				}
			}
		}
	}
}

func TestScrollGeometryDoesNotMutateEditor(t *testing.T) {
	e := New()
	e.Area.Focus()
	e.SetValue(strings.Repeat("界e\u0301 text\n", 20))
	e.Resize(17, 4)
	e.Scroll(-3)
	text, row, col := e.Value(), e.Area.Line(), e.Area.Column()
	offset, height, width := e.Area.ScrollYOffset(), e.Area.Height(), e.Area.Width()
	info := e.Area.LineInfo()
	cursor := *e.Area.Cursor()
	for range 3 {
		copy := e // View works with a shallow editor copy.
		gotOffset, total, browsing := copy.ScrollPosition()
		if gotOffset != offset || total <= height || !browsing {
			t.Fatalf("bad scroll geometry: offset=%d total=%d browsing=%v", gotOffset, total, browsing)
		}
	}
	if e.Value() != text || e.Area.Line() != row || e.Area.Column() != col || e.Area.ScrollYOffset() != offset || e.Area.Height() != height || e.Area.Width() != width || e.Area.LineInfo() != info || *e.Area.Cursor() != cursor {
		t.Fatal("geometry query changed draft, cursor or viewport")
	}
	e.SetValue("short")
	e.Resize(17, 4)
	if offset, total, browsing := e.ScrollPosition(); offset != 0 || total != 1 || browsing {
		t.Fatalf("stale geometry after draft replacement: %d %d %v", offset, total, browsing)
	}
}

func BenchmarkScrollPosition(b *testing.B) {
	for _, size := range []int{4096, 32768} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			e := New()
			e.SetValue(strings.Repeat("text ", size/5))
			e.Resize(76, 5)
			b.SetBytes(int64(len(e.Value())))
			b.ReportAllocs()
			for b.Loop() {
				if _, total, _ := e.ScrollPosition(); total < 1 {
					b.Fatal("missing visual lines")
				}
			}
		})
	}
}
