package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

const reportFixture = `{"status":"completed","summary":["恢复中间活动","保留 **Markdown**"],"changed_files":["a` + "`" + `.go"],"validation":[{"command":"go test ./...","result":"passed","details":"全部通过","custom":9007199254740993}],"acceptance":[{"criterion":"完整显示","result":"failed","evidence":"尚有问题"}],"findings":[{"severity":"P2","problem":"问题细节"}],"risks":["未进行真实终端验收"],"out_of_scope":["部署"],"recommended_next_action":["检查结果"],"unknown":{"number":900719925474099312345,"nested":[true,null]}}`

func TestWorkerReportMarkdown(t *testing.T) {
	got := workerReportMarkdown(reportFixture)
	for _, want := range []string{"**状态：** completed", "## 摘要", "- 恢复中间活动", "## 验证", "go test ./...", "passed", "## 验收", "failed", "尚有问题", "## 风险", "未进行真实终端验收", "## 改动文件", "## 发现", "问题细节", "## 范围外事项", "## 后续建议", "## 其他字段", "9007199254740993", "900719925474099312345", "\"nested\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if workerReportMarkdown("```json\n"+reportFixture+"\n```") != got {
		t.Fatal("fenced report differs")
	}
	for _, source := range []string{`{"status":"completed"}`, `{"x":1}`, `[1,2]`, `{"status":"completed","summary":[`, reportFixture + " trailing", "# Normal Markdown", "```json\n{\"path\":\"a.go\"}\n```"} {
		if workerReportMarkdown(source) != source {
			t.Fatalf("rewrote non-report %q", source)
		}
	}
	odd := workerReportMarkdown(`{"status":"custom","summary":[{"unexpected":42},null,7],"validation":[],"acceptance":[{}]}`)
	for _, want := range []string{"custom", "unexpected", "42", "null", "7", "{}"} {
		if !strings.Contains(odd, want) {
			t.Fatalf("lost atypical field %s", want)
		}
	}
	if strings.Contains(odd, "## 验证") {
		t.Fatal("empty list section retained")
	}
}

func TestWorkerReportOnlyLastAssistantAndImmutable(t *testing.T) {
	items := []session.Item{{ID: "old", Role: "assistant", Text: reportFixture}, {ID: "tool", Role: "tool", Text: reportFixture}, {ID: "final", Role: "assistant", Text: reportFixture}}
	formatted := workerReportItems(items)
	if formatted[0].Text != reportFixture || formatted[1].Text != reportFixture || formatted[2].Text == reportFixture {
		t.Fatal("incorrect target")
	}
	if items[2].Text != reportFixture {
		t.Fatal("mutated source")
	}
	items[2].Text = "Ordinary final reply"
	if workerReportItems(items)[0].Text != reportFixture {
		t.Fatal("converted an earlier report")
	}
}

func TestWorkerReportConversationRendering(t *testing.T) {
	m, parent, _ := workerPageFixture(t)
	receiveWorkerCommand(t, m, m.openWorkerSession(parent, "w", "index:batch:0"))
	w := m.inspector.worker
	w.replay = replayFixtureItems([]session.Item{{ID: "user", Role: "user", Text: "Task input", MessageIndex: 1}, {ID: "final", Role: "assistant", Text: reportFixture, MessageIndex: 2}})
	w.refreshVisibleMessages(w.replay.View())
	for _, width := range []int{20, 40, 80} {
		rows := m.workerConversationRows(width)
		plain := ansi.Strip(strings.Join(rows, "\n"))
		for _, want := range []string{"摘要", "验证", "验收", "风险", "恢复中间活动"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("width %d missing %s", width, want)
			}
		}
		if strings.Contains(plain, `{"status"`) || strings.Contains(plain, "**状态：**") {
			t.Fatalf("report was not rendered as Markdown: %s", plain)
		}
		cached := m.workerConversationRows(width)
		if &cached[0] != &rows[0] {
			t.Fatal("cache rebuilt on static view")
		}
	}
	if w.visible[1].Text != reportFixture || w.replay.View().Items[1].Text != reportFixture {
		t.Fatal("changed replay state")
	}
}

func TestWorkerReportCompactSpacing(t *testing.T) {
	source := `{"status":"completed","summary":["first","second"],"validation":[{"command":"go test ./...","result":"passed"},{"command":"git diff --check","result":"passed"}],"acceptance":[{"criterion":"works","result":"passed","evidence":"verified"}]}`
	got := workerReportMarkdown(source)
	if !strings.Contains(got, "- first\n- second\n") {
		t.Fatal("summary became a loose list")
	}
	for _, unwanted := range []string{"\n\n\n", "---", "**结果：**\n", "**证据：**\n", "```"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("unnecessary spacing/block %q in %s", unwanted, got)
		}
	}
	for _, wanted := range []string{"- **结果：** passed\n- **命令：**", "- **条件：** works\n- **结果：** passed\n- **证据：** verified"} {
		if !strings.Contains(got, wanted) {
			t.Fatalf("fields not compact: %s", got)
		}
	}
	multiline := workerReportMarkdown(`{"status":"completed","summary":[],"validation":[{"command":"first\n\nlast"}]}`)
	if !strings.Contains(multiline, "```\nfirst\n\nlast\n```") {
		t.Fatal("lost command content")
	}
}

func TestWorkerReportCodeFenceSafety(t *testing.T) {
	var out strings.Builder
	workerReportFence(&out, "", "echo ``` then ```` end")
	if !strings.HasPrefix(out.String(), "`````\n") {
		t.Fatal("unsafe command fence")
	}
}
