package session

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestForkPlanOwnsPrefixAndRevalidatesSource(t *testing.T) {
	path, snapshot := forkSource(t)
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, snapshot, "", "  "); err != nil {
		t.Fatal(err)
	}
	plan, err := PrepareFork("selected", pretty.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	clear(pretty.Bytes()) // RPC buffer may be reused before the write.
	review := plan.Review()
	if review.Prefix != nil {
		t.Fatal("review exposed writable records")
	}
	review.Prefix = []json.RawMessage{json.RawMessage(`{"id":"injected"}`)}
	review.Turn = 99
	review.LeafID = "injected"
	root := filepath.Dir(path)
	if _, err = plan.PersistNumbered(root, path, "changed", "/tmp", "name"); err == nil {
		t.Fatal("stale identity accepted")
	}
	if _, err = (&ForkPlan{}).PersistNumbered(root, path, "original", "/tmp", "name"); err == nil {
		t.Fatal("unvalidated plan accepted")
	}
	branch, err := plan.createNumbered(root, path, "original", "/tmp", "name", true)
	if err != nil {
		t.Fatal(err)
	}
	if branch.Origin.Fork.Turn != 2 || branch.Origin.Fork.CutoffEntryID != "error" {
		t.Fatalf("review mutated plan: %+v", branch.Origin.Fork)
	}
	raw, err := ReadFile(branch.Path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk, inMemory any
	if err = json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(branch.Entries, &inMemory); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(onDisk, inMemory) {
		t.Fatal("JSONL reload disagrees with fork snapshot")
	}
}
