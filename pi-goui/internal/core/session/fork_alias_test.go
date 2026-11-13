package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestForkResolveAliasIdentity(t *testing.T) {
	snapshot := json.RawMessage(`{"entries":[{"type":"message","id":"u1","parentId":null,"message":{"role":"user","content":"same","timestamp":1}},{"type":"message","id":"u2","parentId":"u1","message":{"role":"user","content":"same","timestamp":2}}],"leafId":"u2"}`)
	var s State
	s.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"user","content":"same","timestamp":1}}`))
	s.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"user","content":"same","timestamp":2}}`))
	selected := s.live[1].id
	checkpoint := s.ForkCheckpoint()
	for _, persisted := range []bool{false, true} {
		if persisted {
			if err := s.LoadEntries(snapshot); err != nil {
				t.Fatal(err)
			}
			checkpoint = s.ForkCheckpoint()
		}
		id, err := checkpoint.ResolveUserID(selected, snapshot)
		if err != nil || id != "u2" {
			t.Fatalf("persisted=%v: id=%q err=%v", persisted, id, err)
		}
		// A sibling containing exactly the same text must not substitute for the
		// selected message. Persisted identity or exact timestamp prevents this.
		sibling := json.RawMessage(strings.NewReplacer(`"u2"`, `"other"`, `"timestamp":2`, `"timestamp":3`).Replace(string(snapshot)))
		id, err = checkpoint.ResolveUserID(selected, sibling)
		if err == nil {
			_, err = PreviewFork(id, sibling)
		}
		if err == nil {
			t.Fatalf("persisted=%v: sibling selection accepted", persisted)
		}
	}
}

func TestForkResolveIdenticalMessages(t *testing.T) {
	var s State
	s.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"user","content":"same","timestamp":1}}`))
	selected := s.live[0].id
	snapshot := json.RawMessage(`{"entries":[{"type":"message","id":"u1","parentId":null,"message":{"role":"user","content":"same","timestamp":1}},{"type":"message","id":"u2","parentId":"u1","message":{"role":"user","content":"same","timestamp":1}}],"leafId":"u2"}`)
	if err := s.LoadEntries(snapshot); err != nil {
		t.Fatal(err)
	}
	second := s.records[1].id
	if err := s.LoadEntries(snapshot); err != nil {
		t.Fatal(err)
	}
	if s.records[0].id != selected || s.records[1].id != second {
		t.Fatal("reloading identical messages changed display identities")
	}
	if id, err := s.ForkCheckpoint().ResolveUserID(selected, snapshot); err != nil || id != "u1" {
		t.Fatalf("first identical message: id=%q err=%v", id, err)
	}
	if id, err := s.ForkCheckpoint().ResolveUserID(s.records[1].id, snapshot); err != nil || id != "u2" {
		t.Fatalf("second identical message: id=%q err=%v", id, err)
	}
}

func TestForkResolveLiveRejectsChangedPrefix(t *testing.T) {
	original := json.RawMessage(`{"entries":[{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"anchor"}}],"leafId":"root"}`)
	var s State
	if err := s.LoadEntries(original); err != nil {
		t.Fatal(err)
	}
	s.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"user","content":"same","timestamp":2}}`))
	selected := s.live[0].id
	sibling := json.RawMessage(`{"entries":[{"type":"message","id":"other-root","parentId":null,"message":{"role":"user","content":"anchor"}},{"type":"message","id":"u2","parentId":"other-root","message":{"role":"user","content":"same","timestamp":2}}],"leafId":"u2"}`)
	if _, err := s.ForkCheckpoint().ResolveUserID(selected, sibling); err == nil {
		t.Fatal("identical content on another branch accepted")
	}
}
