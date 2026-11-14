package ui

import (
	"pi-goui/internal/extensions/hostui"

	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"pi-goui/internal/extensions/toolrender/cards"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/extensions/codexfast"
	rpc "pi-goui/internal/runtime"
)

func TestCodexExtensionProcess(t *testing.T) {
	mode := os.Getenv("GOUI_CODEX_EXTENSION_CHILD")
	if mode == "" {
		return
	}
	wire, _ := os.OpenFile(os.Getenv("GOUI_CODEX_EXTENSION_WIRE"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	defer wire.Close()
	level := "normal"
	scanner := bufio.NewScanner(os.Stdin)
	emit := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	for scanner.Scan() {
		var request map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &request)
		_, _ = fmt.Fprintln(wire, scanner.Text())
		data := any(map[string]any{})
		switch request["type"] {
		case "get_commands":
			commands := []map[string]string{}
			if mode != "absent" {
				commands = append(commands, map[string]string{"name": "pi-ui-rpc", "source": "extension"})
			}
			data = map[string]any{"commands": commands}
		case "prompt":
			if mode == "absent" {
				os.Exit(5)
			}
			var call struct {
				ID, Method string
				Params     struct{ Level string }
			}
			_ = json.Unmarshal([]byte(strings.TrimPrefix(request["message"].(string), "/pi-ui-rpc ")), &call)
			response := map[string]any{"id": call.ID}
			if mode == "missing" {
				response["error"] = map[string]string{"code": "unavailable", "message": "capability absent"}
			} else if mode == "readonly" && call.Method == "codex-fast.set" {
				response["error"] = map[string]string{"code": "save-failed", "message": "disk readonly"}
			} else {
				if call.Method == "codex-fast.set" {
					level = call.Params.Level
				}
				response["result"] = map[string]any{"level": level, "levels": hostui.CodexFastLevels}
			}
			raw, _ := json.Marshal(response)
			emit(map[string]any{"type": "extension_ui_request", "method": "notify", "message": "pi-ui:rpc:v1:" + string(raw)})
			data = map[string]string{"disposition": "handled"}
		}
		emit(map[string]any{"type": "response", "id": request["id"], "success": true, "data": data})
	}
	os.Exit(0)
}
func codexClient(t *testing.T, mode string) (*rpc.Client, string) {
	t.Helper()
	root := t.TempDir()
	wire := filepath.Join(root, "wire")
	t.Setenv("GOUI_CODEX_EXTENSION_CHILD", mode)
	t.Setenv("GOUI_CODEX_EXTENSION_WIRE", wire)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	client, err := rpc.Start(rpc.Config{Bin: executable, Args: []string{"-test.run=^TestCodexExtensionProcess$", "--"}, AgentDir: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, wire
}
func codexState(level string) codexfast.State {
	return codexfast.State{Level: level, Levels: append([]string{}, hostui.CodexFastLevels...)}
}
func loadCodexState(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.pollExtensions(time.Now())
	if cmd == nil {
		t.Fatal("missing state request")
	}
	m.Update(cmd())
}

func TestCodexFastAbsentAndIndependentUserDirectory(t *testing.T) {
	for _, mode := range []string{"absent", "missing", "normal"} {
		t.Run(mode, func(t *testing.T) {
			m := testModel(t, "")
			m.cfg.AgentDir = t.TempDir()
			// Existing unrelated user's configuration must never be read or modified.
			path := filepath.Join(m.cfg.AgentDir, "codex-fast.json")
			const untouched = "not JSON; frontend must not parse this"
			if err := os.WriteFile(path, []byte(untouched), 0600); err != nil {
				t.Fatal(err)
			}
			client, wire := codexClient(t, mode)
			m.current().client = client
			if m.current().extensions[0].(*hostui.Session).Codex.Mode(m.current().dead) != "" || m.sidebarLayout().ModeRow < 0 || m.sidebarContext().Mode != "—" {
				t.Fatal("missing default mode before discovery")
			}
			loadCodexState(t, m)
			expected := ""
			if mode == "normal" {
				expected = "normal"
			}
			if m.current().extensions[0].(*hostui.Session).Codex.Mode(m.current().dead) != expected {
				t.Fatalf("mode = %q", m.current().extensions[0].(*hostui.Session).Codex.Mode(m.current().dead))
			}
			label := "—"
			if mode == "normal" {
				label = "normal x1"
			}
			if m.sidebarLayout().ModeRow < 0 || m.sidebarContext().Mode != label {
				t.Fatal("missing default mode after discovery")
			}
			if mode != "normal" && m.current().extensions[0].(*hostui.Session).Codex.State.Available() {
				t.Fatal("default label enabled absent capability")
			}
			raw, _ := os.ReadFile(path)
			if string(raw) != untouched {
				t.Fatal("frontend touched extension config")
			}
			raw, _ = os.ReadFile(wire)
			if mode == "absent" && strings.Contains(string(raw), `"prompt"`) {
				t.Fatal("unknown command could start model")
			}
		})
	}
}

func TestCodexModeDoesNotAnnotateModelName(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	tab := m.current()
	tab.state.Model.ID = "fast-model-ultrafast"
	tab.state.Thinking = "high"
	before := ansi.Strip(m.inputFooter(tab).text)
	for _, level := range hostui.CodexFastLevels {
		tab.extensions[0].(*hostui.Session).Codex.State = codexState(level)
		if got := ansi.Strip(m.inputFooter(tab).text); got != before {
			t.Fatal("mode altered model label")
		}
	}
}

func TestCodexFastPopupSelectionWaitsForBackend(t *testing.T) {
	m := testModel(t, "")
	m.cfg.AgentDir = t.TempDir()
	client, _ := codexClient(t, "normal")
	m.current().client = client
	loadCodexState(t, m)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	target, box, ok := m.extensionPopupGeometry()
	if !ok || box.y+box.height != target.y || box.x < m.mainWidth() || box.x+box.width > m.width {
		t.Fatal("invalid popup geometry")
	}
	for _, x := range []int{m.mainWidth() + 2, target.x - 1, target.x + target.width} {
		m.Update(tea.MouseMotionMsg{X: x, Y: target.y})
		if m.extensionPopupVisible() {
			t.Fatal("padding opened popup")
		}
	}
	for i, level := range hostui.CodexFastLevels {
		target, _, _ = m.extensionPopupGeometry()
		m.Update(tea.MouseMotionMsg{X: target.x, Y: target.y})
		m.Update(tea.MouseMotionMsg{X: box.x + 2, Y: box.y + 1 + i})
		before := m.current().extensions[0].(*hostui.Session).Codex.Mode(m.current().dead)
		cmd, handled := m.extensionClickCommand(box.x+2, box.y+1+i)
		if !handled || cmd == nil {
			t.Fatal("no RPC action")
		}
		if m.current().extensions[0].(*hostui.Session).Codex.Mode(m.current().dead) != before {
			t.Fatal("optimistic display before commit")
		}
		m.Update(cmd())
		if m.current().extensions[0].(*hostui.Session).Codex.Mode(m.current().dead) != level {
			t.Fatalf("backend level not displayed: %s", level)
		}
		_, newBox, _ := m.extensionPopupGeometry()
		if box != newBox {
			t.Fatal("popup moved")
		}
		line := strings.Split(ansi.Strip(m.View().Content), "\n")[m.sidebarLayout().ModeRow]
		if !strings.Contains(line, cards.CodexModeLabel(level)) {
			t.Fatal("missing mode label")
		}
	}
	files, _ := os.ReadDir(m.cfg.AgentDir)
	if len(files) != 0 {
		t.Fatal("frontend persisted extension config")
	}
}
func TestCodexFastFailureAndPerTabState(t *testing.T) {
	m := testModel(t, "")
	client, _ := codexClient(t, "readonly")
	first := m.current()
	first.client = client
	loadCodexState(t, m)
	cmd := m.extensionControlView().Request(first.id, first.client, first.dead, "fast", time.Now())
	m.Update(cmd())
	if first.extensions[0].(*hostui.Session).Codex.State.Level != "normal" || !strings.Contains(first.toast.text, "failed") {
		t.Fatal("failed save changed state")
	}
	m.addTab("")
	if m.current().extensions[0].(*hostui.Session).Codex.Mode(m.current().dead) != "" {
		t.Fatal("new backend inherited another tab's state")
	}
	first.extensions[0].(*hostui.Session).Codex.Pending = true
	m.Update(hostui.CodexResult{ID: first.id, Version: first.extensions[0].(*hostui.Session).Codex.Version, State: codexState("ultrafast")})
	if m.current() == first || m.current().extensions[0].(*hostui.Session).Codex.Mode(m.current().dead) != "" || first.extensions[0].(*hostui.Session).Codex.State.Level != "ultrafast" {
		t.Fatal("response applied to wrong tab")
	}
}

func TestCodexSettingsRequestUsesLocalMenuAndBackendSave(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	tab := m.current()
	client, wire := fakeClient(t)
	tab.client = client
	raw := json.RawMessage(`{"type":"extension_ui_request","id":"codex-1","method":"input","title":"pi-goui:codex-fast:v1","placeholder":"{\"level\":\"normal\"}"}`)
	m.extension(tab, raw)
	if len(tab.dialogs) != 1 {
		t.Fatal("missing settings dialog")
	}
	d := tab.dialogs[0]
	if d.kind != "select" || !strings.Contains(d.title, "Codex 档位") || len(d.options) != 3 {
		t.Fatal("Go menu not initialized")
	}
	d.selected = 1
	commandResult(m.confirmDialog(tab, d))
	records := wireRecords(t, wire, 1)
	if records[0]["id"] != "codex-1" || records[0]["value"] != "fast" {
		t.Fatal("incorrect settings response")
	}
	if _, err := os.Stat(filepath.Join(m.cfg.AgentDir, "codex-fast.json")); !os.IsNotExist(err) {
		t.Fatal("RPC dialog must leave persistence to backend")
	}
}

func TestCodexSettingsCancellationAndMalformedPayload(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	m.extension(tab, json.RawMessage(`{"method":"input","title":"pi-goui:codex-fast:v1","placeholder":"{\"level\":\"fast\"}"}`))
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEsc}, "esc")
	if len(tab.dialogs) != 0 {
		t.Fatal("cancel kept settings open")
	}
	m.extension(tab, json.RawMessage(`{"method":"input","title":"pi-goui:codex-fast:v1","placeholder":"{\"level\":\"invalid\"}"}`))
	if len(tab.dialogs) != 0 {
		t.Fatal("invalid protocol became editable raw JSON")
	}
}
