package session

import (
	"fmt"
	"reflect"
	"testing"
)

func TestThreadRowsRecentFamiliesAndLegacy(t *testing.T) {
	list := []Summary{
		{ID: "child", Path: "/child", ParentSession: "/parent", Modified: 30},
		{ID: "other", Path: "/other", Modified: 20},
		{ID: "parent", Path: "/parent", Modified: 1},
		{ID: "old", Path: "/old", ParentSession: "/parent", Modified: 5},
		{ID: "grand", Path: "/grand", ParentSession: "/old", Modified: 40},
	}
	want := []ThreadRow{{2, 0}, {3, 1}, {4, 2}, {0, 1}, {1, 0}}
	if got := ThreadRows(list); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows: %v", got)
	}
	if list[0].ID != "child" {
		t.Fatal("mutated input")
	}
	if got := ThreadRows(list[:1]); !reflect.DeepEqual(got, []ThreadRow{{0, 0}}) {
		t.Fatalf("missing parent: %v", got)
	}
}
func TestThreadRowsMalformedAndDeep(t *testing.T) {
	list := []Summary{
		{ID: "a", Path: "/a", ParentSession: "/b"}, {ID: "b", Path: "/b", ParentSession: "/a"},
		{ID: "self", Path: "/self", ParentSession: "/self"},
		{ID: "dup", Path: "/dup"}, {ID: "other", Path: "/dup"},
		{ID: "c", Path: "/c", ParentSession: "/dup"},
		{ID: "d", Path: "/d", ParentSession: "/a", Fork: &ForkProvenance{SourceID: "wrong"}},
		{ID: "a", Path: "/another"},
	}
	rows := ThreadRows(list)
	seen := map[int]bool{}
	for _, r := range rows {
		if seen[r.Index] {
			t.Fatal("duplicate row")
		}
		seen[r.Index] = true
		if r.Index >= 2 && r.Depth != 0 {
			t.Fatalf("invalid edge: %v", r)
		}
	}
	if len(rows) != len(list) {
		t.Fatal("lost entries")
	}
	deep := make([]Summary, 5000)
	for i := range deep {
		deep[i] = Summary{ID: fmt.Sprint(i), Path: fmt.Sprintf("/%d", i)}
		if i > 0 {
			deep[i].ParentSession = deep[i-1].Path
		}
	}
	rows = ThreadRows(deep)
	if len(rows) != len(deep) || rows[len(rows)-1].Depth != len(deep)-1 {
		t.Fatal("deep chain lost")
	}
	if ThreadPrefix(0) != "" || ThreadPrefix(1) != "↳ " || ThreadPrefix(5000) != "      ↳ " {
		t.Fatal("unbounded prefix")
	}
}
