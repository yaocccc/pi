package input

import (
	"fmt"
	"strings"
	"testing"
)

func TestWordPartsRetainGraphemeSpans(t *testing.T) {
	for _, text := range []string{
		strings.Repeat("x", 65536),
		strings.Repeat("e\u0301", 8192),
		"alpha\u0301-beta 👩🏽‍💻 \t世界\nnext!",
		"...((one))---two!!! \u00a0third",
	} {
		parts := piWordParts(text)
		var reconstructed strings.Builder
		for _, part := range parts {
			reconstructed.WriteString(part.text)
		}
		if reconstructed.String() != text {
			t.Fatal("word parts changed original grapheme bytes")
		}
	}
	long := strings.Repeat("x", 65536)
	parts := piWordParts(long)
	if len(parts) != 1 || parts[0].text != long || parts[0].kind != 1 {
		t.Fatal("long word was split or changed")
	}
	if piWordBoundary(long, len(long), true) != 0 || piWordBoundary(long, 0, false) != len(long) {
		t.Fatal("long word boundaries changed")
	}
}

func BenchmarkWordBoundary(b *testing.B) {
	for _, size := range []int{2048, 8192, 32768, 65536} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			text := strings.Repeat("x", size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				if piWordBoundary(text, size, true) != 0 {
					b.Fatal("wrong boundary")
				}
			}
		})
	}
}
