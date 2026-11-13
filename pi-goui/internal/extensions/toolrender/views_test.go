package toolrender_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/toolrender"
)

func TestViewsAreOptInAndNativeToolsRemainNative(t *testing.T) {
	tools := toolrender.NewViews()
	if tools == toolrender.NewViews() {
		t.Fatal("providers must have independent identity")
	}
	question := session.Item{ID: "q", Role: "tool", ToolName: "ask_question", HasResult: true, Arguments: json.RawMessage(`{"question":"CHOOSE_LANGUAGE","options":["Go","TS"]}`)}
	plain := strings.Join(render.ToolResultRows(question, config.Theme{}, 80), "\n")
	rich := strings.Join(render.ToolResultRows(question, config.Theme{}, 80, tools), "\n")
	if !strings.Contains(rich, "Question\nCHOOSE_LANGUAGE") || rich == plain {
		t.Fatalf("provider not explicitly installed: plain=%q rich=%q", plain, rich)
	}
	for _, name := range []string{"read", "write", "edit", "bash", "codemode"} {
		item := session.Item{ID: "native", Role: "tool", ToolName: name, HasResult: true, Text: "NATIVE_RESULT", Arguments: json.RawMessage(`{"code":"1 + 1","command":"printf ok","path":"sample.txt"}`)}
		if with, without := render.ToolResultRows(item, config.Theme{}, 80, tools), render.ToolResultRows(item, config.Theme{}, 80); !reflect.DeepEqual(with, without) {
			t.Fatalf("%s was overridden: %v != %v", name, with, without)
		}
	}
}
