package cards

import (
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/session"
	"testing"
)

func TestPresentationPolicyOwnsOnlySpecializedCards(t *testing.T) {
	for _, tc := range []struct {
		name string
		want extensionapi.CardPolicy
	}{
		{"worker", extensionapi.CardPolicy{Specialized: true, DefaultExpanded: true}},
		{"tools.WORKER", extensionapi.CardPolicy{Specialized: true, DefaultExpanded: true}},
		{"ask_question", extensionapi.CardPolicy{Specialized: true, Compact: true}},
		{"tools.ASK_QUESTION", extensionapi.CardPolicy{Specialized: true, Compact: true}},
		{"read", extensionapi.CardPolicy{}}, {"write", extensionapi.CardPolicy{}},
		{"edit", extensionapi.CardPolicy{}}, {"bash", extensionapi.CardPolicy{}},
		{"shell", extensionapi.CardPolicy{}}, {"powershell", extensionapi.CardPolicy{}},
		{"codemode", extensionapi.CardPolicy{}}, {"unknown", extensionapi.CardPolicy{}},
		{"worker_extra", extensionapi.CardPolicy{}},
	} {
		if got := Policy(tc.name); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestUnknownAndNativeToolsDoNotInvokeExtensionCallbacks(t *testing.T) {
	// A zero host panics if a renderer is incorrectly claimed. No global host or
	// registry can influence this instance or take ownership of native tools.
	r := New(extensionapi.CardHost{})
	for _, name := range []string{"read", "write", "edit", "bash", "codemode", "custom", "worker_extra"} {
		item := session.Item{ToolName: name}
		if rows, blocks, ok := r.Layout(item, config.Theme{}, Options{}, 80); ok || rows != nil || blocks != nil {
			t.Errorf("layout claimed %s", name)
		}
		if rows, ok := r.Detail(item, config.Theme{}, 80); ok || rows != nil {
			t.Errorf("detail claimed %s", name)
		}
	}
}
