package ui

import (
	"pi-goui/internal/extensions/hostui"

	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/rpc"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/worker"
)

const workerPageID = "12345678-1234-4234-8234-123456789abc"

func workerPageFixture(t *testing.T) (*Model, *tab, string) {
	t.Helper()
	m := workerTaskFixture(t, true)
	parent := m.current()
	root := t.TempDir()
	m.cfg.AgentDir = filepath.Join(root, "agent")
	dir := filepath.Join(root, "worker-transcripts")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, workerPageID+".jsonl")
	header := `{"type":"worker_transcript_header","version":1,"id":"` + workerPageID + `","cwd":"/tmp","entries":[]}` + "\n"
	if err := os.WriteFile(path, []byte(header), 0600); err != nil {
		t.Fatal(err)
	}
	parent.state.Items[0].Details = json.RawMessage(`{"kind":"worker-ui","batchId":"batch","tasks":[{"index":0,"title":"任务甲 🧪","mode":"implement","preset":"deep","status":"running","transcript":{"version":1,"id":"` + workerPageID + `"}},{"index":1,"title":"Other task","status":"running"}]}`)
	m.document(parent)
	parent.offset = 0
	parent.follow = false
	parent.client = workerTranscriptTestClient(t, path)
	return m, parent, path
}
func appendWorkerLog(t *testing.T, path, text string) {
	t.Helper()
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.WriteString(text); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
}
func receiveWorkerCommand(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing read command")
	}
	msg, ok := cmd().(hostui.WorkerTranscriptResult)
	if !ok {
		t.Fatal("unexpected command")
	}
	m.receiveInspectorMessage(msg)
	m.document(m.current())
}

const workerLiveEvents = `{"type":"agent_start"}
{"type":"message_start","message":{"role":"user","content":"真实任务输入","timestamp":1}}
{"type":"message_end","message":{"role":"user","content":"真实任务输入","timestamp":1}}
{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":2}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"实时中文 🧪"}}
{"type":"tool_execution_start","toolCallId":"r1","toolName":"read","args":{"path":"a.go"}}
{"type":"tool_execution_end","toolCallId":"r1","toolName":"read","result":{"content":[{"type":"text","text":"FULL TOOL RESULT"}]},"isError":false}
`

func TestWorkerWithoutExtensionBackendNeverReadsLocalRecordings(t *testing.T) {
	m := workerTaskFixture(t, true)
	parent := m.current()
	root := t.TempDir()
	m.cfg.AgentDir = filepath.Join(root, "agent")
	if err := os.Mkdir(filepath.Join(root, "worker-transcripts"), 0700); err != nil {
		t.Fatal(err)
	}
	text := `{"type":"worker_transcript_header","version":1,"id":"` + workerPageID + `","entries":[]}` + "\n" + `{"type":"message_end","message":{"role":"user","content":"PRIVATE_LOCAL_RECORDING","timestamp":1}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "worker-transcripts", workerPageID+".jsonl"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	parent.state.Items[0].Details = json.RawMessage(`{"kind":"worker-ui","batchId":"batch","tasks":[{"index":0,"title":"Optional task","status":"completed","transcript":{"version":1,"id":"` + workerPageID + `"}}]}`)
	receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
	if (m.inspector.content != nil && m.inspector.content.(*hostui.Inspector).Worker != nil) || !strings.Contains(m.inspector.content.(*hostui.Inspector).Fallback, "不可用") {
		t.Fatal("missing backend did not fall back")
	}
	if strings.Contains(strings.Join(m.inspectorRows(), "\n"), "PRIVATE_LOCAL_RECORDING") {
		t.Fatal("frontend read a local recording without RPC")
	}
}

func TestWorkerConversationInspectorLiveAndCopy(t *testing.T) {
	m, parent, path := workerPageFixture(t)
	original, _ := json.Marshal(parent.state)
	rect := workerTaskButton(t, m, "index:batch:0")
	parent.selection = transcriptSelection{anchor: textPoint{line: 1}, end: textPoint{line: 2}, moved: true, document: parent.doc.Text}
	savedSelection := parent.selection
	click := tea.Mouse{Button: tea.MouseLeft, X: rect.x + rect.width - 1, Y: rect.y}
	cmd := m.mouse(click)
	m.mouseRelease(click)
	if m.current() != parent || len(m.tabs) != 1 || !m.inspectorOpen() || (m.inspector.content == nil || m.inspector.content.(*hostui.Inspector).Worker == nil) {
		t.Fatal("task did not open beside parent")
	}
	if parent.offset != 0 || parent.follow || parent.selection.anchor != savedSelection.anchor || !parent.selection.moved {
		t.Fatal("opening changed parent scroll/selection")
	}
	if m.pollExtensionInspectors(time.Now()) != nil {
		t.Fatal("duplicate read")
	}
	receiveWorkerCommand(t, m, cmd)
	appendWorkerLog(t, path, workerLiveEvents)
	m.inspector.content.(*hostui.Inspector).Worker.Next = time.Time{}
	receiveWorkerCommand(t, m, m.pollExtensionInspectors(time.Now()))
	rows := m.inspectorRows()
	all := ansi.Strip(strings.Join(rows, "\n"))
	for _, text := range []string{"真实任务输入", "实时中文"} {
		if !strings.Contains(all, text) {
			t.Fatalf("missing %q in %s", text, all)
		}
	}
	foundTool := false
	for _, item := range m.inspector.content.(*hostui.Inspector).Worker.Visible {
		foundTool = foundTool || strings.Contains(item.Text, "FULL TOOL RESULT")
	}
	if !foundTool || len(m.inspector.content.(*hostui.Inspector).Worker.Visible) <= 2 {
		t.Fatal("task detail lost intermediate tools")
	}
	if strings.Contains(all, "Other task") {
		t.Fatal("task isolation")
	}
	after, _ := json.Marshal(parent.state)
	if string(original) != string(after) {
		t.Fatal("recording mutated parent state")
	}
	// Scrolling and mouse selection reuse the complete cached conversation.
	left, _ := m.inspectorBounds()
	for i := 0; i < 10; i++ {
		m.inspectorWheel(tea.Mouse{Button: tea.MouseWheelDown, X: left + 3, Y: 5})
		m.inspectorView()
	}
	if &m.inspectorRows()[0] != &rows[0] {
		t.Fatal("scroll rerendered conversation")
	}
	found := false
	for i, line := range rows {
		plain := ansi.Strip(line)
		at := strings.Index(plain, "实时中文")
		if at < 0 {
			continue
		}
		m.inspector.offset = min(i, max(0, len(rows)-m.inspectorBodyHeight()))
		y := 2 + i - m.inspector.offset
		x := left + m.inspectorInset() + ansi.StringWidth(plain[:at])
		m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
		m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: x + 3, Y: y})
		if got := clipboardText(t, m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: x + 3, Y: y})); got != "实时" {
			t.Fatalf("copy=%q", got)
		}
		found = true
		break
	}
	if !found {
		t.Fatal("no result to copy")
	}
	if parent.offset != 0 || !parent.selection.moved {
		t.Fatal("inspector interaction altered parent")
	}
	appendWorkerLog(t, path, "{\"type\":\"worker_transcript_end\",\"status\":\"completed\",\"complete\":true}\n")
	m.inspector.content.(*hostui.Inspector).Worker.Next = time.Time{}
	receiveWorkerCommand(t, m, m.pollExtensionInspectors(time.Now()))
	if !m.inspector.content.(*hostui.Inspector).Worker.Replay.Stopped || m.inspector.content.(*hostui.Inspector).Worker.Replay.Status != "completed" {
		t.Fatal("missing terminal")
	}
	if m.pollExtensionInspectors(time.Now().Add(time.Hour)) != nil {
		t.Fatal("terminal polling")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.inspectorOpen() || m.current() != parent || len(m.tabs) != 1 || !parent.state.Items[0].Running {
		t.Fatal("close affected parent")
	}
}
func TestWorkerSummaryFallbackLifecycle(t *testing.T) {
	m, parent, path := workerPageFixture(t)
	beforeTabs, beforeOffset, beforeClient := len(m.tabs), parent.offset, parent.client
	snapshot := func(reversed bool, transcript string, summary string) {
		t.Helper()
		a := `{"index":0,"title":"TASK_A","objective":"ONLY_A_GOAL","mode":"implement","status":"completed","resolvedPreset":"deep","allowedPaths":["SECRET_CONTRACT"],"activities":[{"id":"SECRET_ID","at":123,"detail":"SECRET_ACTIVITY"}],"result":{"summary":["` + summary + `"],"validation":[{"command":"go test ./...","result":"passed"}],"risks":["ONLY_A_RISK"],"execution":{"internal":"SECRET_EXECUTION"}}` + transcript + `}`
		b := `{"index":1,"title":"ONLY_B_PRIVATE","result":{"summary":["ONLY_B_RESULT"]}}`
		tasks := a + "," + b
		if reversed {
			tasks = b + "," + a
		}
		parent.state.Items[0].Details = json.RawMessage(`{"kind":"worker-ui","batchId":"batch","tasks":[` + tasks + `]}`)
	}
	snapshot(false, "", "FIRST_SUMMARY")
	if m.openExtensionInspector(parent, "w", "index:batch:0") != nil || (m.inspector.content != nil && m.inspector.content.(*hostui.Inspector).Worker != nil) {
		t.Fatal("old snapshot started a recording read")
	}
	plain := ansi.Strip(strings.Join(m.inspectorRows(), "\n"))
	for _, want := range []string{"简要详情", "完整会话记录不可用", "未保存", "ONLY_A_GOAL", "FIRST_SUMMARY", "go test", "ONLY_A_RISK", "deep"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %s", want, plain)
		}
	}
	for _, secret := range []string{"SECRET_", "ONLY_B_", "Activity"} {
		if strings.Contains(plain, secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
	rows := m.inspector.rows
	m.inspectorRows()
	if &rows[0] != &m.inspector.rows[0] {
		t.Fatal("unchanged fallback rebuilt")
	}
	snapshot(true, "", "UPDATED_SUMMARY")
	plain = ansi.Strip(strings.Join(m.inspectorRows(), "\n"))
	if !strings.Contains(plain, "UPDATED_SUMMARY") || strings.Contains(plain, "ONLY_B_") {
		t.Fatal("reorder or live summary failed")
	}
	if m.current() != parent || len(m.tabs) != beforeTabs || parent.offset != beforeOffset || parent.client != beforeClient {
		t.Fatal("fallback changed parent/session")
	}
	// A later descriptor upgrades only on explicit re-open, never hot polling.
	snapshot(true, `,"transcript":{"version":1,"id":"`+workerPageID+`"}`, "UPDATED_SUMMARY")
	if m.pollExtensionInspectors(time.Now().Add(time.Hour)) != nil {
		t.Fatal("fallback hot polling")
	}
	appendWorkerLog(t, path, workerLiveEvents)
	receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
	if (m.inspector.content == nil || m.inspector.content.(*hostui.Inspector).Worker == nil) || m.inspector.content.(*hostui.Inspector).Fallback != "" || len(m.inspector.content.(*hostui.Inspector).Worker.Visible) == 0 {
		t.Fatal("reopen did not upgrade to real recording")
	}
	m.closeInspector()
	snapshot(false, "", "FINAL")
	m.openExtensionInspector(parent, "w", "index:batch:0")
	parent.state.Items[0].Details = json.RawMessage(`{"kind":"worker-ui","tasks":[]}`)
	m.document(parent)
	if m.inspectorOpen() {
		t.Fatal("removed task kept summary open")
	}
}

func TestWorkerFallbackReadFailuresAndRecovery(t *testing.T) {
	for _, kind := range []string{"invalid-id", "malformed", "permission", "missing"} {
		t.Run(kind, func(t *testing.T) {
			m, parent, path := workerPageFixture(t)
			switch kind {
			case "invalid-id":
				parent.state.Items[0].Details = json.RawMessage(strings.ReplaceAll(string(parent.state.Items[0].Details), workerPageID, "../../private"))
			case "malformed":
				if err := os.WriteFile(path, []byte("not-json\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			cmd := m.openExtensionInspector(parent, "w", "index:batch:0")
			if kind == "invalid-id" {
				if cmd != nil {
					t.Fatal("unsafe descriptor attempted disk read")
				}
			} else if kind == "permission" {
				m.receiveInspectorMessage(hostui.WorkerTranscriptResult{Owner: m.inspector.content.(*hostui.Inspector).Worker, Err: &rpc.ExtensionError{Code: "transcript-permission", Message: "access denied"}})
			} else {
				receiveWorkerCommand(t, m, cmd)
			}
			if !m.inspectorOpen() || (m.inspector.content != nil && m.inspector.content.(*hostui.Inspector).Worker != nil) || m.inspector.content.(*hostui.Inspector).Fallback == "" {
				t.Fatal("failure did not open summary")
			}
			plain := ansi.Strip(strings.Join(m.inspectorRows(), "\n"))
			if !strings.Contains(plain, "简要详情") || strings.Contains(plain, "PRIVATE") || strings.Contains(plain, "../../") {
				t.Fatal("bad or unsafe summary")
			}
			if m.pollExtensionInspectors(time.Now().Add(time.Hour)) != nil {
				t.Fatal("failure hot loop")
			}
		})
	}
}

func TestWorkerDescriptorChangeRejectsPendingRecording(t *testing.T) {
	m, parent, path := workerPageFixture(t)
	appendWorkerLog(t, path, workerLiveEvents)
	cmd := m.openExtensionInspector(parent, "w", "index:batch:0")
	parent.state.Items[0].Details = json.RawMessage(strings.ReplaceAll(string(parent.state.Items[0].Details), workerPageID, "22345678-1234-4234-8234-123456789abc"))
	receiveWorkerCommand(t, m, cmd)
	if (m.inspector.content != nil && m.inspector.content.(*hostui.Inspector).Worker != nil) || !strings.Contains(m.inspector.content.(*hostui.Inspector).Fallback, "关联已变化") {
		t.Fatal("old pending record displayed under new descriptor")
	}
}

func TestWorkerRecordingErrorRetainsReadMessages(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		m, parent, path := workerPageFixture(t)
		appendWorkerLog(t, path, workerLiveEvents)
		receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
		w := m.inspector.content.(*hostui.Inspector).Worker
		m.inspectorRows()
		var before []string
		for _, item := range w.Visible {
			before = append(before, item.Text)
		}
		if replacement {
			if err := os.WriteFile(path, []byte("invalid replacement\n"), 0600); err != nil {
				t.Fatal(err)
			}
		} else {
			appendWorkerLog(t, path, "invalid tail\n")
		}
		w.Next = time.Time{}
		receiveWorkerCommand(t, m, m.pollExtensionInspectors(time.Now()))
		m.inspectorRows()
		var after []string
		for _, item := range w.Visible {
			after = append(after, item.Text)
		}
		if m.inspector.content.(*hostui.Inspector).Worker != w || strings.Join(before, "\n") != strings.Join(after, "\n") || !strings.Contains(w.Replay.Status, "保留已读取内容") {
			t.Fatalf("read failure discarded real conversation replacement=%v before=%q after=%q status=%s", replacement, before, after, w.Replay.Status)
		}
		if m.pollExtensionInspectors(time.Now().Add(time.Hour)) != nil {
			t.Fatal("partial failure hot loop")
		}
		// Explicit re-open retries a repaired log with a fresh response owner.
		header := `{"type":"worker_transcript_header","version":1,"id":"` + workerPageID + `","entries":[]}` + "\n"
		if err := os.WriteFile(path, []byte(header+workerLiveEvents), 0600); err != nil {
			t.Fatal(err)
		}
		receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
		if m.inspector.content.(*hostui.Inspector).Worker == w || m.inspector.content.(*hostui.Inspector).Fallback != "" {
			t.Fatal("read failure could not be retried")
		}
		m.receiveInspectorMessage(hostui.WorkerTranscriptResult{Owner: w, Err: &rpc.ExtensionError{Code: "transcript-not-found", Message: "unavailable"}})
		if m.inspector.content.(*hostui.Inspector).Fallback != "" {
			t.Fatal("stale failure downgraded reopened recording")
		}
	}
}

func TestWorkerConversationStaleReadAndMissingRecording(t *testing.T) {
	m, parent, path := workerPageFixture(t)
	cmd := m.openExtensionInspector(parent, "w", "index:batch:0")
	old := m.inspector.content.(*hostui.Inspector).Worker
	m.closeInspector()
	fresh := m.openExtensionInspector(parent, "w", "index:batch:0")
	m.receiveInspectorMessage(cmd().(hostui.WorkerTranscriptResult))
	if m.inspector.content.(*hostui.Inspector).Worker == old || !m.inspector.content.(*hostui.Inspector).Worker.Pending || m.inspector.content.(*hostui.Inspector).Worker.Revision != 1 {
		t.Fatal("stale read applied")
	}
	receiveWorkerCommand(t, m, fresh)
	m.closeInspector()
	_ = os.Remove(path)
	receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
	if (m.inspector.content != nil && m.inspector.content.(*hostui.Inspector).Worker != nil) || !m.inspectorOpen() || !strings.Contains(m.inspector.content.(*hostui.Inspector).Fallback, "不存在") {
		t.Fatal("missing file not reported")
	}
	if m.pollExtensionInspectors(time.Now().Add(time.Hour)) != nil {
		t.Fatal("missing file hot loop")
	}
	m.closeInspector()
	if cmd := m.openExtensionInspector(parent, "w", "index:batch:1"); cmd != nil || !m.inspectorOpen() || (m.inspector.content != nil && m.inspector.content.(*hostui.Inspector).Worker != nil) || len(m.tabs) != 1 {
		t.Fatal("legacy task used fake activity")
	}
}
func TestWorkerConversationSwitchIgnoresPendingReads(t *testing.T) {
	m, parent, path := workerPageFixture(t)
	first := m.openExtensionInspector(parent, "w", "index:batch:0")
	const secondID = "22345678-1234-4234-8234-123456789abc"
	secondPath := filepath.Join(filepath.Dir(path), secondID+".jsonl")
	if err := os.WriteFile(secondPath, []byte(strings.ReplaceAll(`{"type":"worker_transcript_header","version":1,"id":"ID","cwd":"/tmp","entries":[]}`, "ID", secondID)+"\n"+workerLiveEvents), 0600); err != nil {
		t.Fatal(err)
	}
	parent.state.Items[0].Details = json.RawMessage(`{"kind":"worker-ui","batchId":"batch","tasks":[{"index":0,"status":"running","transcript":{"version":1,"id":"` + workerPageID + `"}},{"index":1,"status":"running","transcript":{"version":1,"id":"` + secondID + `"}}]}`)
	second := m.openExtensionInspector(parent, "w", "index:batch:1")
	m.receiveInspectorMessage(first().(hostui.WorkerTranscriptResult))
	if m.inspector.content.(*hostui.Inspector).Worker.Descriptor.ID != secondID || !m.inspector.content.(*hostui.Inspector).Worker.Pending {
		t.Fatal("old task read overwrote new task")
	}
	receiveWorkerCommand(t, m, second)
	if len(m.tabs) != 1 || m.inspector.content.(*hostui.Inspector).Key != "index:batch:1" || len(m.inspector.content.(*hostui.Inspector).Worker.Replay.View().Items) == 0 {
		t.Fatal("new task not displayed")
	}
	m.inspector.content.(*hostui.Inspector).Worker.Next = time.Time{}
	pending := m.pollExtensionInspectors(time.Now())
	m.addTab("")
	m.activate(1)
	m.receiveInspectorMessage(pending().(hostui.WorkerTranscriptResult))
	if m.inspectorOpen() || (m.inspector.content != nil && m.inspector.content.(*hostui.Inspector).Worker != nil) {
		t.Fatal("old session read reopened inspector")
	}
}

func TestWorkerConversationIncompleteAndTaskLifecycle(t *testing.T) {
	for _, marker := range []string{"", "{\"type\":\"worker_transcript_end\",\"status\":\"incomplete\",\"complete\":false,\"error\":\"size_limit\"}\n"} {
		m, parent, path := workerPageFixture(t)
		receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
		appendWorkerLog(t, path, marker)
		m.inspector.content.(*hostui.Inspector).Worker.Settled = time.Now().Add(-5 * time.Second)
		m.inspector.content.(*hostui.Inspector).Worker.Next = time.Time{}
		receiveWorkerCommand(t, m, m.pollExtensionInspectors(time.Now()))
		if !m.inspector.content.(*hostui.Inspector).Worker.Replay.Stopped || !strings.Contains(m.inspector.content.(*hostui.Inspector).Worker.Replay.Status, "Incomplete") {
			t.Fatal("false completion")
		}
	}
	m, parent, _ := workerPageFixture(t)
	receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
	parent.state.Items[0].Details = json.RawMessage(`{"kind":"worker-ui","batchId":"batch","tasks":[{"index":1,"title":"Other"},{"index":0,"title":"Renamed","status":"running","transcript":{"version":1,"id":"` + workerPageID + `"}}]}`)
	if !m.inspectorOpen() || m.inspector.content.(*hostui.Inspector).Title != "Renamed" {
		t.Fatal("reorder changed task")
	}
	pending := m.pollExtensionInspectors(time.Now().Add(time.Hour))
	parent.state.Items[0].Details = json.RawMessage(`{"kind":"worker-ui","batchId":"batch","tasks":[]}`)
	if m.inspectorOpen() {
		t.Fatal("removed task survived")
	}
	if pending != nil {
		m.receiveInspectorMessage(pending().(hostui.WorkerTranscriptResult))
	}
	if (m.inspector.content != nil && m.inspector.content.(*hostui.Inspector).Worker != nil) && len(m.inspector.content.(*hostui.Inspector).Worker.Replay.View().Items) != 0 {
		t.Fatal("removed task accepted read")
	}
}
func TestWorkerConversationWidthDragAndLiveCache(t *testing.T) {
	m, parent, path := workerPageFixture(t)
	appendWorkerLog(t, path, workerLiveEvents)
	receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
	rows := m.inspectorRows()
	oldRevision := m.inspector.content.(*hostui.Inspector).Worker.Rendered
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: m.mainWidth(), Y: 2})
	for _, width := range []int{70, 80, 90, 75} {
		m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: m.width - width, Y: 2})
		m.View()
		if &m.inspectorRows()[0] != &rows[0] || m.inspector.content.(*hostui.Inspector).Worker.Rendered != oldRevision {
			t.Fatal("drag rebuilt conversation")
		}
	}
	appendWorkerLog(t, path, "{\"type\":\"message_start\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"LIVE_AFTER_RESIZE\"}],\"timestamp\":10}}\n")
	m.inspector.content.(*hostui.Inspector).Worker.Next = time.Time{}
	receiveWorkerCommand(t, m, m.pollExtensionInspectors(time.Now()))
	if &m.inspectorRows()[0] != &rows[0] {
		t.Fatal("live event rebuilt while dragging")
	}
	m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: m.width - 75, Y: 2})
	if !strings.Contains(ansi.Strip(strings.Join(m.inspectorRows(), "\n")), "LIVE_AFTER_RESIZE") {
		t.Fatal("release did not reflow latest data")
	}
	for _, width := range []int{1, 2, 30, 59, 60, 120} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		for _, row := range strings.Split(m.View().Content, "\n") {
			if ansi.StringWidth(row) > width {
				t.Fatalf("width %d: %q", width, row)
			}
		}
		if m.current() != parent || len(m.tabs) != 1 {
			t.Fatal("narrow view became tab")
		}
	}
}

func TestWorkerConversationFollowsUntilManualScroll(t *testing.T) {
	m, parent, _ := workerPageFixture(t)
	receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
	w := m.inspector.content.(*hostui.Inspector).Worker
	appendText := func() {
		t.Helper()
		if len(w.Visible) == 0 {
			w.Visible = []session.Item{{ID: "reply", Role: "assistant"}}
		}
		w.Visible[len(w.Visible)-1].Text += strings.Repeat("streamed line\n\n", 40)
		w.Revision++
		m.inspectorRows()
	}
	bottom := func() int { return max(0, len(m.inspector.rows)-m.inspectorBodyHeight()) }
	appendText()
	if bottom() == 0 || m.inspector.offset != bottom() {
		t.Fatal("initial content did not follow to bottom")
	}
	appendText()
	if m.inspector.offset != bottom() {
		t.Fatal("streamed content did not follow")
	}
	left, _ := m.inspectorBounds()
	wheel := func(button tea.MouseButton) {
		t.Helper()
		if !m.inspectorWheel(tea.Mouse{Button: button, X: left + m.inspectorInset(), Y: 3}) {
			t.Fatal("wheel not handled")
		}
	}
	wheel(tea.MouseWheelUp)
	offset := m.inspector.offset
	appendText()
	if !m.inspector.followPaused || m.inspector.offset != offset {
		t.Fatal("manual upward scroll did not pause following")
	}
	for m.inspector.offset < bottom() {
		wheel(tea.MouseWheelDown)
	}
	appendText()
	if m.inspector.followPaused || m.inspector.offset != bottom() {
		t.Fatal("returning to bottom did not resume following")
	}
	m.closeInspector()
	receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
	if m.inspector.followPaused {
		t.Fatal("new inspector inherited paused follow")
	}
}

func TestWorkerConversationActivityCache(t *testing.T) {
	w := &hostui.WorkerSession{}
	view := session.State{Items: []session.Item{
		{ID: "live-tool", Role: "tool", ToolID: "new", Arguments: json.RawMessage(`{"x":1}`), Details: json.RawMessage(`{"x":1}`)},
	}}
	w.RefreshVisibleMessages(view)
	if len(w.Visible) != 1 || w.Visible[0].ToolID != "new" {
		t.Fatal("projected live tool lost from the display cache")
	}
	checkChange := func(mutate func()) {
		t.Helper()
		revision := w.Revision
		mutate()
		w.RefreshVisibleMessages(view)
		if w.Revision != revision+1 {
			t.Fatal("activity change did not invalidate cache")
		}
		w.RefreshVisibleMessages(view)
		if w.Revision != revision+1 {
			t.Fatal("unchanged activity invalidated cache")
		}
	}
	checkChange(func() { view.Items[0].Arguments[5] = '2' })
	checkChange(func() { view.Items[0].Details[5] = '2' })
	checkChange(func() { view.Items[0].Running = true })
	checkChange(func() { view.Items[0].Thinking = "progress" })
	checkChange(func() { view.Busy = true })
	checkChange(func() { view.Items[0].MessageIndex = 3 })
	if len(w.Visible) != 1 {
		t.Fatal("attributed tool duplicated")
	}
}

func TestWorkerConversationTaskMessages(t *testing.T) {
	for _, inherited := range []int{0, 120} {
		t.Run(fmt.Sprintf("inherited-%d", inherited), func(t *testing.T) {
			m, parent, _ := workerPageFixture(t)
			receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
			w := m.inspector.content.(*hostui.Inspector).Worker
			deliver := func(reset bool, records ...map[string]any) {
				t.Helper()
				batch := worker.Batch{Reset: reset}
				for _, record := range records {
					raw, err := json.Marshal(record)
					if err != nil {
						t.Fatal(err)
					}
					batch.Records = append(batch.Records, raw)
				}
				m.receiveInspectorMessage(hostui.WorkerTranscriptResult{Owner: w, Batch: batch})
				m.inspectorRows()
			}
			message := func(kind, role string, stamp int, content ...map[string]any) map[string]any {
				return map[string]any{"type": kind, "message": map[string]any{"role": role, "timestamp": stamp, "content": content}}
			}
			text := func(value string) map[string]any { return map[string]any{"type": "text", "text": value} }
			entries := make([]map[string]any, 0, inherited)
			for i := 0; i < inherited; i++ {
				role := "user"
				if i%2 == 1 {
					role = "assistant"
				}
				entry := map[string]any{"type": "message", "id": fmt.Sprint(i), "message": message("", role, i+1, text("INHERITED_SECRET"))["message"]}
				if i > 0 {
					entry["parentId"] = fmt.Sprint(i - 1)
				}
				entries = append(entries, entry)
			}
			header := map[string]any{"type": "worker_transcript_header", "version": 1, "id": workerPageID, "entries": entries}
			deliver(true, header)
			if w.Replay.View().MessageCount != inherited || len(w.Visible) != 0 {
				t.Fatalf("fork context leaked: count=%d visible=%+v", w.Replay.View().MessageCount, w.Visible)
			}
			deliver(false, message("message_start", "user", 1000, text("CURRENT_TASK_INPUT")), message("message_end", "user", 1000, text("CURRENT_TASK_INPUT")))
			if len(w.Visible) != 1 || w.Visible[0].Text != "CURRENT_TASK_INPUT" {
				t.Fatal("task input missing or duplicated")
			}
			deliver(false, message("message_end", "assistant", 1001, map[string]any{"type": "thinking", "thinking": "HIDDEN_THINKING"}, map[string]any{"type": "toolCall", "id": "call", "name": "read", "arguments": map[string]any{"path": "a.go"}}))
			if len(w.Visible) < 3 {
				t.Fatal("thinking/tool-only activity was discarded")
			}
			deliver(false, message("message_end", "assistant", 1002, text("EARLIER_REPLY")))
			if w.Visible[len(w.Visible)-1].Text != "EARLIER_REPLY" {
				t.Fatal("readable reply missing")
			}
			rows, revision := m.inspector.rows, w.Revision
			deliver(false,
				map[string]any{"type": "tool_execution_start", "toolCallId": "call", "toolName": "read", "args": map[string]any{"path": "a.go"}},
				map[string]any{"type": "tool_execution_end", "toolCallId": "call", "toolName": "read", "result": map[string]any{"content": []map[string]any{text("HIDDEN_TOOL_RESULT")}}, "isError": false},
				message("message_end", "assistant", 1003, map[string]any{"type": "thinking", "thinking": "MORE_HIDDEN_THINKING"}))
			if w.Revision == revision || &rows[0] == &m.inspector.rows[0] {
				t.Fatal("internal activity did not invalidate the document")
			}
			rows, revision = m.inspector.rows, w.Revision
			w.RefreshVisibleMessages(w.Replay.View())
			m.inspectorRows()
			if w.Revision != revision || &rows[0] != &m.inspector.rows[0] {
				t.Fatal("unchanged activity invalidated the document")
			}
			deliver(false, message("message_start", "assistant", 1004))
			foundEarlier := false
			for _, item := range w.Visible {
				foundEarlier = foundEarlier || item.Text == "EARLIER_REPLY"
			}
			if !foundEarlier {
				t.Fatal("empty streaming message displaced readable reply")
			}
			deliver(false, map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "contentIndex": 0, "delta": "# Final conclusion\n\n**bold** result"}})
			if !strings.Contains(w.Visible[len(w.Visible)-1].Text, "Final conclusion") {
				t.Fatal("latest streamed text not selected")
			}
			conclusion := "# Final conclusion\n\n**bold** result\n\n- checked\n- done\n\n| Name | Result |\n| --- | --- |\n| test | pass |\n\n```go\n" + strings.Repeat("var 中文 = 1\n", 40) + "// FINAL_END\n```"
			deliver(false, message("message_end", "assistant", 1004, map[string]any{"type": "thinking", "thinking": "HIDDEN_THINKING"}, text(conclusion), text("FINAL_TAIL")), map[string]any{"type": "worker_transcript_end", "status": "completed", "complete": true})
			var allText string
			var toolFound, thinkingFound bool
			for _, item := range w.Visible {
				allText += item.Text + "\n"
				toolFound = toolFound || item.ToolID == "call" && item.Text == "HIDDEN_TOOL_RESULT" && len(item.Arguments) > 0
				thinkingFound = thinkingFound || strings.Contains(item.Thinking, "HIDDEN_THINKING")
			}
			if !strings.Contains(allText, conclusion) || !strings.Contains(allText, "FINAL_TAIL") || !toolFound || !thinkingFound {
				t.Fatal("task result or intermediate activity not preserved in full")
			}
			plain := ansi.Strip(w.Doc.Text)
			for _, hidden := range []string{"INHERITED_SECRET", "**bold**"} {
				if strings.Contains(plain, hidden) {
					t.Fatalf("unwanted output %q", hidden)
				}
			}
			for _, wanted := range []string{"CURRENT_TASK_INPUT", "EARLIER_REPLY", "Final conclusion", "bold", "checked", "FINAL_END", "FINAL_TAIL"} {
				if !strings.Contains(plain, wanted) {
					t.Fatalf("missing formatted output %q", wanted)
				}
			}
			if w.Replay.View().MessageCount <= inherited+2 {
				t.Fatal("filter destroyed full recording state")
			}
			if len(w.Doc.Blocks) <= 2 {
				t.Fatal("intermediate calls missing from document")
			}
			// Replacement/truncation restarts the inherited boundary and display.
			header["entries"] = []map[string]any{}
			deliver(true, header)
			if w.Replay.View().MessageCount != 0 || len(w.Visible) != 0 || strings.Contains(w.Doc.Text, "FINAL_TAIL") {
				t.Fatal("reset retained previous task messages")
			}
			deliver(false, message("message_end", "user", 2000, text("REPLACED_INPUT")))
			if len(w.Visible) != 1 || w.Visible[0].Text != "REPLACED_INPUT" {
				t.Fatal("reset boundary suppressed new input")
			}
		})
	}
}

func BenchmarkWorkerConversation(b *testing.B) {
	for _, count := range []int{200, 1000} {
		for _, phase := range []string{"scroll-view", "motion-view", "hover-view", "selection-view", "gesture"} {
			b.Run(fmt.Sprintf("%d/%s", count, phase), func(b *testing.B) {
				m := resizeBenchmarkModel(b, "worker", 1)
				var items []session.Item
				for i := 0; i < count; i++ {
					items = append(items, session.Item{ID: fmt.Sprint(i), Role: "assistant", Text: "完整消息 👩‍💻 This is a message with some source context", MessageIndex: i})
				}
				args, _ := json.Marshal(map[string]any{"path": "large.go", "content": strings.Repeat("var value = \"完整源码 👩‍💻 with sufficient length to wrap\"\n", count)})
				items = append(items, session.Item{ID: "write", Role: "tool", ToolName: "write", Arguments: args, HasResult: true})
				items = append(items, session.Item{ID: "task-input", Role: "user", Text: "Explain the result", MessageIndex: count + 1}, session.Item{ID: "final", Role: "assistant", Text: "# Conclusion\n\n```go\n" + strings.Repeat("var value = \"完整源码 👩‍💻 with sufficient length to wrap\"\n", count) + "```", MessageIndex: count + 2})
				m.inspector.content = &hostui.Inspector{Worker: &hostui.WorkerSession{Replay: replayFixtureItems(items), Revision: 1}}
				m.inspector.content.(*hostui.Inspector).Worker.RefreshVisibleMessages(m.inspector.content.(*hostui.Inspector).Worker.Replay.View())
				m.inspectorRows()
				m.View()
				press := func() { m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.mainWidth(), Y: 2}) }
				if phase == "motion-view" {
					press()
				}
				if phase == "selection-view" {
					m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.mainWidth() + 5, Y: 4})
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					switch phase {
					case "scroll-view":
						m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: m.width - 8, Y: 10})
						m.View()
					case "hover-view":
						m.Update(tea.MouseMotionMsg{X: m.mainWidth() + 5 + i%10, Y: 5})
						m.View()
					case "selection-view":
						m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: m.mainWidth() + 5 + i%10, Y: 6})
						m.View()
					case "motion-view":
						m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: m.width - (45 + i%25), Y: 2})
						m.View()
					default:
						press()
						for step := 0; step < 10; step++ {
							m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: m.width - (45 + step*3), Y: 2})
							m.View()
						}
						m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: m.width - 72, Y: 2})
						m.View()
					}
				}
			})
		}
	}
}

// A test-only fake Pi backend makes the host tests independent of this repository's
// TypeScript extensions. Files below are backend fixture storage, never frontend IO.
func workerTranscriptTestClient(t *testing.T, recording string) *rpc.Client {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX RPC test wrapper")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bin := filepath.Join(root, "pi")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nWORKER_TRANSCRIPT_HELPER=" + quote(recording) + " exec " + quote(exe) + " -test.run=^TestWorkerTranscriptRPCBackend$ -- \"$@\"\n"
	if err = os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := rpc.Start(rpc.Config{Bin: bin, Cwd: root, AgentDir: root, NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
func TestWorkerTranscriptRPCBackend(t *testing.T) {
	name := os.Getenv("WORKER_TRANSCRIPT_HELPER")
	if name == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	type position struct {
		data   []byte
		offset int
		info   os.FileInfo
	}
	positions := map[string]position{}
	serial := 0
	for scanner.Scan() {
		var command struct{ ID, Type, Message string }
		if json.Unmarshal(scanner.Bytes(), &command) != nil {
			continue
		}
		if command.Type == "get_commands" {
			_ = out.Encode(map[string]any{"type": "response", "id": command.ID, "success": true, "data": map[string]any{"commands": []any{map[string]any{"name": "pi-ui-rpc", "source": "extension"}}}})
			continue
		}
		var request struct {
			ID, Method string
			Params     struct {
				Descriptor worker.Transcript
				Cursor     string
			}
		}
		_ = json.Unmarshal([]byte(strings.TrimPrefix(command.Message, "/pi-ui-rpc ")), &request)
		response := map[string]any{"id": request.ID}
		recording := filepath.Join(filepath.Dir(name), request.Params.Descriptor.ID+".jsonl")
		data, err := os.ReadFile(recording)
		info, _ := os.Stat(recording)
		if err != nil {
			response["error"] = map[string]any{"code": "transcript-not-found", "message": "recording unavailable"}
		} else {
			previous, exists := positions[request.Params.Cursor]
			reset := !exists || previous.info == nil || !os.SameFile(previous.info, info) || len(data) < previous.offset || !bytes.Equal(data[:min(previous.offset, len(data))], previous.data[:min(previous.offset, len(previous.data))])
			start := previous.offset
			if reset {
				start = 0
			}
			pending := data[start:]
			complete := bytes.LastIndexByte(pending, '\n') + 1
			records := []json.RawMessage{}
			invalid := false
			for _, line := range bytes.Split(pending[:complete], []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				if !json.Valid(line) {
					invalid = true
					break
				}
				records = append(records, json.RawMessage(line))
			}
			if invalid {
				response["error"] = map[string]any{"code": "invalid-transcript", "message": "invalid recording"}
			} else {
				serial++
				token := fmt.Sprintf("00000000-0000-0000-0000-%012d", serial)
				positions[token] = position{data: data, offset: start + complete, info: info}
				response["result"] = map[string]any{"cursor": token, "reset": reset, "records": records, "more": false, "partial": complete < len(pending)}
			}
		}
		encoded, _ := json.Marshal(response)
		_ = out.Encode(map[string]any{"type": "extension_ui_request", "method": "notify", "message": "pi-ui:rpc:v1:" + string(encoded), "notifyType": "info"})
		_ = out.Encode(map[string]any{"type": "response", "id": command.ID, "success": true, "data": map[string]any{"disposition": "handled"}})
	}
	os.Exit(0)
}
