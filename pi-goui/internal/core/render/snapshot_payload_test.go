package render

import (
	"fmt"
	"testing"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/session"
)

// The same ToolID survives a partial result, a late replacement, then a
// branch refresh. Hold an older layout open inside a hostile card provider.
func TestNativePayloadVersionsInConcurrentLayout(t *testing.T) {
	var state session.State
	branch := func(leaf, value string) {
		t.Helper()
		raw := fmt.Sprintf(`{"entries":[{"type":"message","id":"%s-call","message":{"role":"assistant","content":[{"type":"toolCall","id":"t","name":"example","arguments":{"arg":%q}}]}},{"type":"message","id":%q,"parentId":"%s-call","message":{"role":"toolResult","toolCallId":"t","toolName":"example","content":"ok","details":{"result":%q}}}]}`, leaf, value, leaf, leaf, value)
		if err := state.LoadEntries([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	branch("left", "left")
	old := state.Items[0].LayoutSnapshot()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		var c TranscriptCache
		views := &ToolViews{Layout: func(_ extensionapi.CardHost, it session.Item, _ config.Theme, _ extensionapi.CardOptions, _ int) ([]string, []extensionapi.Block, bool) {
			close(entered)
			<-release
			it.Arguments[0] = 'X'
			it.Details[0] = 'X'
			return []string{"mutated private view"}, nil, true
		}}
		c.Transcript([]session.Item{old}, testTheme(), Options{Width: 80, Tools: views, ExpandTools: true})
	}()
	select {
	case <-entered:
	case <-done:
		t.Fatal("card provider not called")
	}
	original := state.Items[0]
	branch("right", "right")
	close(release)
	<-done
	for _, check := range []struct {
		item  session.Item
		value string
	}{{original, "left"}, {old.MaterializeLayoutJSON(), "left"}, {state.Items[0].LayoutSnapshot().MaterializeLayoutJSON(), "right"}} {
		if string(check.item.Arguments) != fmt.Sprintf(`{"arg":%q}`, check.value) || string(check.item.Details) != fmt.Sprintf(`{"result":%q}`, check.value) {
			t.Fatalf("aliased/stale version: %s %s", check.item.Arguments, check.item.Details)
		}
	}
	// The public RawMessages can themselves be retained/mutated by an extension
	// while the immutable layout input remains the native projection version.
	state.Items[0].Arguments[0] = 'x'
	if got := state.Items[0].LayoutSnapshot().MaterializeLayoutJSON(); got.Arguments[0] != '{' {
		t.Fatal("mutable view reached native layout version")
	}
	var live session.State
	live.Apply([]byte(`{"type":"tool_execution_update","toolCallId":"t","toolName":"example","args":{"x":1},"partialResult":{"details":{"n":1}}}`))
	first := live.Items[0].LayoutSnapshot()
	live.Apply([]byte(`{"type":"tool_execution_end","toolCallId":"t","result":{"details":{"n":2}}}`))
	second := live.Items[0].LayoutSnapshot()
	live.Apply([]byte(`{"type":"tool_execution_end","toolCallId":"t","result":{"details":{"n":3}}}`))
	third := live.Items[0].LayoutSnapshot()
	for n, it := range []session.Item{first, second, third} {
		it = it.MaterializeLayoutJSON()
		if string(it.Arguments) != `{"x":1}` || string(it.Details) != fmt.Sprintf(`{"n":%d}`, n+1) {
			t.Fatal("lost/inherited wrong field version", string(it.Arguments), string(it.Details))
		}
	}
}
