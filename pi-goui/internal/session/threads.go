package session

import (
	"path/filepath"
	"sort"
	"strings"
)

// ThreadRow retains the input index so presentation never changes action targets.
type ThreadRow struct{ Index, Depth int }

// ThreadRows groups parents before descendants, with families and siblings
// ordered by their most recent activity. Missing or ambiguous parents are roots.
func ThreadRows(list []Summary) []ThreadRow {
	n := len(list)
	parent, colors, recent := make([]int, n), make([]uint8, n), make([]int64, n)
	byPath := map[string]int{}
	clean := func(p string) string {
		if p == "" {
			return ""
		}
		return filepath.Clean(p)
	}
	for i, s := range list {
		parent[i] = -1
		recent[i] = s.Modified
		p := clean(s.Path)
		if p == "" {
			continue
		}
		if _, ok := byPath[p]; ok {
			byPath[p] = -1
		} else {
			byPath[p] = i
		}
	}
	for i, s := range list {
		p, ok := byPath[clean(s.ParentSession)]
		if !ok || p < 0 || p == i {
			continue
		}
		if s.Fork != nil && s.Fork.SourceID != "" && s.Fork.SourceID != list[p].ID {
			continue
		}
		parent[i] = p
	}
	// Break cycles iteratively; even corrupt/deep histories keep every row.
	for start := range list {
		var path []int
		at := start
		for at >= 0 && colors[at] == 0 {
			colors[at] = 1
			path = append(path, at)
			at = parent[at]
		}
		if at >= 0 && colors[at] == 1 {
			parent[at] = -1
		}
		for _, i := range path {
			colors[i] = 2
		}
	}
	children := make([][]int, n)
	var roots []int
	for i, p := range parent {
		if p < 0 {
			roots = append(roots, i)
		} else {
			children[p] = append(children[p], i)
		}
	}
	var order []int
	stack := append([]int(nil), roots...)
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		order = append(order, i)
		stack = append(stack, children[i]...)
	}
	for j := len(order) - 1; j >= 0; j-- {
		i := order[j]
		if p := parent[i]; p >= 0 && recent[i] > recent[p] {
			recent[p] = recent[i]
		}
	}
	less := func(rows []int) {
		sort.SliceStable(rows, func(i, j int) bool { return recent[rows[i]] > recent[rows[j]] })
	}
	less(roots)
	for _, rows := range children {
		less(rows)
	}
	pending := make([]ThreadRow, 0, n)
	for i := len(roots) - 1; i >= 0; i-- {
		pending = append(pending, ThreadRow{Index: roots[i]})
	}
	result := make([]ThreadRow, 0, n)
	for len(pending) > 0 {
		row := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		result = append(result, row)
		for j := len(children[row.Index]) - 1; j >= 0; j-- {
			pending = append(pending, ThreadRow{Index: children[row.Index][j], Depth: row.Depth + 1})
		}
	}
	return result
}

// ThreadPrefix is deliberately bounded, not a precise tree diagram.
func ThreadPrefix(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("  ", min(depth-1, 3)) + "↳ "
}
