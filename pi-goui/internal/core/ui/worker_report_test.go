package ui

import (
	"pi-goui/internal/extensions/hostui"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

const reportFixture = `{"status":"completed","summary":["恢复中间活动","保留 **Markdown**"],"changed_files":["a` + "`" + `.go"],"validation":[{"command":"go test ./...","result":"passed","details":"全部通过","custom":9007199254740993}],"acceptance":[{"criterion":"完整显示","result":"failed","evidence":"尚有问题"}],"findings":[{"severity":"P2","problem":"问题细节"}],"risks":["未进行真实终端验收"],"out_of_scope":["部署"],"recommended_next_action":["检查结果"],"unknown":{"number":900719925474099312345,"nested":[true,null]}}`

func TestWorkerReportConversationRendering(t *testing.T) {
	m, parent, _ := workerPageFixture(t)
	receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
	w := m.inspector.content.(*hostui.Inspector).Worker
	w.Replay = replayFixtureItems([]session.Item{{ID: "user", Role: "user", Text: "Task input", MessageIndex: 1}, {ID: "final", Role: "assistant", Text: reportFixture, MessageIndex: 2}})
	w.RefreshVisibleMessages(w.Replay.View())
	for _, width := range []int{20, 40, 80} {
		rows := m.extensionConversationRows(width)
		plain := ansi.Strip(strings.Join(rows, "\n"))
		for _, want := range []string{"摘要", "验证", "验收", "风险", "恢复中间活动"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("width %d missing %s", width, want)
			}
		}
		if strings.Contains(plain, `{"status"`) || strings.Contains(plain, "**状态：**") {
			t.Fatalf("report was not rendered as Markdown: %s", plain)
		}
		cached := m.extensionConversationRows(width)
		if &cached[0] != &rows[0] {
			t.Fatal("cache rebuilt on static view")
		}
	}
	if w.Visible[1].Text != reportFixture || w.Replay.View().Items[1].Text != reportFixture {
		t.Fatal("changed replay state")
	}
}
