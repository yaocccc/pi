package hostui

import (
	"encoding/json"
	"fmt"
	"pi-goui/internal/core/session"
	"strings"
	"testing"
)

func inputList(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "- path-%02d.go\n", i)
	}
	return b.String()
}

func TestWorkerInputContractFormatting(t *testing.T) {
	files := []string{}
	for i := 0; i < 12; i++ {
		files = append(files, fmt.Sprintf("file-%d.go", i))
	}
	raw, _ := json.Marshal(map[string]any{"objective": "修复 **问题**", "mode": "implement", "allowedPaths": files, "preExistingUserChanges": files, "acceptanceCriteria": []string{"tests pass"}})
	original := WorkerContractPrefix + string(raw) + "\n最终仅返回 JSON；格式：\n{\"status\":\"completed\"}"
	got := WorkerInputMarkdown(original)
	formatted, actions := WorkerInputItems([]session.Item{{ID: "contract", Role: "user", Text: original}}, nil)
	if len(actions) != 2 || strings.Contains(formatted[0].Text, "file-11.go") || !strings.Contains(formatted[0].Text, "展开剩余 7 项") {
		t.Fatal("actual contract file arrays not folded")
	}
	for _, want := range []string{"目标", "修复 **问题**", "允许修改", "已有改动", "file-11.go", "tests pass", "最终仅返回 JSON；格式：", "\"status\": \"completed\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	for _, original := range []string{"normal **markdown**", `{"path":"a.go"}`, `{"objective":"work","allowedPaths":[`, `{"objective":"work"} garbage`} {
		if WorkerInputMarkdown(original) != original {
			t.Fatal("rewrote non-contract", original)
		}
	}
	got = WorkerInputMarkdown(`{"objective":"work","unknown":{"n":90071992547409931234},"allowedPaths":[1,null,{"a":true}]}`)
	for _, want := range []string{"90071992547409931234", "null", "true"} {
		if !strings.Contains(got, want) {
			t.Fatal("lost unknown value", want)
		}
	}
}

func TestWorkerInputListsPreserveBlocks(t *testing.T) {
	marker := func() string { return "\ue000" }
	for _, source := range []string{inputList(8), inputList(12) + "lazy continuation of last item", "```md\n" + inputList(12) + "```", "~~~\n" + inputList(12) + "~~~", "    - code\n    - code"} {
		got, actions := WorkerInputLists(source, "u", nil, marker)
		if got != source || len(actions) != 0 {
			t.Fatal("changed short list/code", got)
		}
	}
	source := inputList(12)
	folded, actions := WorkerInputLists(source, "u", nil, marker)
	if len(actions) != 1 || !strings.Contains(folded, "path-04.go") || strings.Contains(folded, "path-05.go") || !strings.Contains(folded, "展开剩余 7 项") {
		t.Fatal("bad folded list", folded)
	}
	expanded, actions := WorkerInputLists(source, "u", map[string]bool{actions[0].Key: true}, marker)
	if !strings.Contains(expanded, "path-11.go") || !strings.Contains(expanded, "收起列表") {
		t.Fatal("missing expanded content")
	}
	// Appending items to a streamed list preserves identity and expansion.
	grown, grownActions := WorkerInputLists(source+"- added.go\n", "u", map[string]bool{actions[0].Key: true}, marker)
	if grownActions[0].Key != actions[0].Key || !strings.Contains(grown, "added.go") || !strings.Contains(grown, "收起列表") {
		t.Fatal("streamed list lost expansion")
	}
	_, other := WorkerInputLists(source, "other-message", nil, marker)
	if other[0].Key == actions[0].Key {
		t.Fatal("messages share list state")
	}
	nested := ""
	for i := 0; i < 10; i++ {
		nested += fmt.Sprintf("%d. outer-%d\n   - inner-%d\n\n   continuation-%d\n", i+1, i, i, i)
	}
	got, _ := WorkerInputLists(nested, "u", nil, marker)
	if !strings.Contains(got, "inner-4") || !strings.Contains(got, "continuation-4") || strings.Contains(got, "inner-5") {
		t.Fatal("split nested item", got)
	}
}
