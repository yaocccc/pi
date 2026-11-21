package render

import (
	"slices"
	"sort"
	"strings"
)

// DocumentRows is a comparable snapshot identity. Its segments and their row
// storage are immutable after publication; Range returns caller-owned headers.
type DocumentRows struct{ index *rowIndex }
type rowIndex struct {
	parts []rowPart
	bytes int
}
type rowPart struct {
	start int
	lines []string
	spans []ContentSpan
}

func PlainRows(lines []string) DocumentRows { return RowsWithSpans(lines, nil) }
func TextRows(text string) DocumentRows     { return PlainRows(strings.Split(text, "\n")) }

// RowsWithSpans takes ownership of both slices; neither may be modified after publication.
func RowsWithSpans(lines []string, spans []ContentSpan) DocumentRows {
	if len(lines) == 0 {
		return DocumentRows{}
	}
	size := len(lines) - 1
	for _, s := range lines {
		size += len(s)
	}
	return DocumentRows{&rowIndex{[]rowPart{{0, lines, spans}}, size}}
}
func (r DocumentRows) ByteLen() int {
	if r.index == nil {
		return 0
	}
	return r.index.bytes
}
func (r DocumentRows) LineCount() int {
	if r.index == nil {
		return 1
	}
	p := r.index.parts[len(r.index.parts)-1]
	return p.start + len(p.lines)
}
func (r DocumentRows) part(y int) int {
	return sort.Search(len(r.index.parts), func(i int) bool { return r.index.parts[i].start > y }) - 1
}
func (r DocumentRows) Line(y int) string {
	if r.index == nil {
		return ""
	}
	p := r.index.parts[r.part(y)]
	return p.lines[y-p.start]
}
func (r DocumentRows) Span(y int) (ContentSpan, bool) {
	if r.index != nil && y >= 0 && y < r.LineCount() {
		p := r.index.parts[r.part(y)]
		if n := y - p.start; n < len(p.spans) {
			return p.spans[n], true
		}
	}
	return ContentSpan{}, false
}
func (r DocumentRows) Range(start, end int) []string {
	out := make([]string, 0, end-start)
	if start == end {
		return out
	}
	if r.index == nil {
		return append(out, "")
	}
	for i := r.part(start); start < end; i++ {
		p := r.index.parts[i]
		n := min(end-start, len(p.lines)-(start-p.start))
		out = append(out, p.lines[start-p.start:start-p.start+n]...)
		start += n
	}
	return out
}
func (r DocumentRows) FullText() string {
	if r.index == nil {
		return ""
	}
	var b strings.Builder
	b.Grow(r.ByteLen())
	for _, p := range r.index.parts {
		for i, s := range p.lines {
			if i+p.start > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(s)
		}
	}
	return b.String()
}
func (idx *rowIndex) append(r DocumentRows, start int) {
	if r.index == nil {
		r = TextRows("")
	}
	for _, p := range r.index.parts {
		p.start += start
		idx.parts = append(idx.parts, p)
	}
	idx.bytes += r.ByteLen()
	if start > 0 {
		idx.bytes++
	}
}

// WithBlankAnd appends a display-only prompt echo without copying native rows.
func (r DocumentRows) WithBlankAnd(t DocumentRows) DocumentRows {
	idx := new(rowIndex)
	idx.append(r, 0)
	idx.append(RowsWithSpans([]string{""}, []ContentSpan{{}}), r.LineCount())
	idx.append(t, r.LineCount()+1)
	return DocumentRows{idx}
}

// WithLine changes one display row, e.g. removing a Worker action marker.
func (r DocumentRows) WithLine(y int, s string) DocumentRows {
	idx := *r.index
	idx.parts = slices.Clone(idx.parts)
	p := &idx.parts[r.part(y)]
	p.lines = slices.Clone(p.lines)
	idx.bytes += len(s) - len(p.lines[y-p.start])
	p.lines[y-p.start] = s
	return DocumentRows{&idx}
}
