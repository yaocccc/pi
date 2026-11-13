package protocol

import (
	"encoding/json"
	"testing"
)

func TestUnknownExtensionsKeepStandardFallback(t *testing.T) {
	if Dialog("another-user:form:v9") != "" {
		t.Fatal("unknown dialog must remain a standard Pi dialog")
	}
	if _, recognized := DecodeEvent("ordinary Pi notification"); recognized {
		t.Fatal("ordinary notification must remain visible")
	}
	for _, payload := range []string{`{`, `{"name":"future.event","data":{}}`} {
		event, recognized := DecodeEvent(EventPrefix + payload)
		if !recognized || Notice(event) != "" {
			t.Fatal("unknown or malformed extension facts must not leak protocol text")
		}
	}
}

func TestAdaptersOnlyFormatBusinessFacts(t *testing.T) {
	if Dialog("pi-goui:worker-settings:v1") != WorkerSettings || Dialog("pi-goui:ask-question:v1") != Questionnaire || Dialog("pi-goui:codex-fast:v1") != CodexFastSettings {
		t.Fatal("known dialog routing changed")
	}
	if got := Notice(Event{Name: "worker.settings-saved", Data: json.RawMessage(`{}`)}); got != "Worker 设置已保存，后续任务立即生效" {
		t.Fatalf("unexpected presentation: %q", got)
	}
}
