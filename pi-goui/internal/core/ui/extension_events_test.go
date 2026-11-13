package ui

import (
	"pi-goui/internal/extensions/hostui"
	"strings"
	"testing"
)

func TestExtensionEvents(t *testing.T) {
	for _, tc := range []struct{ event, want string }{
		{`{"name":"context.ready","data":{"generationMs":1200}}`, "生成耗时 1.2s"},
		{`{"name":"context.applied","data":{"beforeTokens":2000,"afterTokens":1000,"generationMs":1200,"durationMs":2300}}`, "减少 1000，50.0%"},
		{`{"name":"context.failed","data":{"durationMs":1200,"reason":"offline"}}`, "原因：offline"},
		{`{"name":"autoname.renamed","data":{"name":"示例","contextTokens":100,"durationMs":1200}}`, "会话已自动命名：示例"},
		{`{"name":"filter.redacted","data":{"path":".env"}}`, "已隐藏敏感文件内容：.env"},
		{`{"name":"worker.settings-saved","data":{}}`, "Worker 设置已保存"},
		{`{"name":"codex.settings-error","data":{"code":"invalid-level"}}`, "无效的档位"},
		{`{"name":"future.event","data":{}}`, ""},
		{`{broken`, ""},
	} {
		t.Run(tc.event, func(t *testing.T) {
			tab := &tab{extensions: extensionSession{&hostui.Session{}}}
			if !tab.extensionEvent("pi-goui:event:v1:" + tc.event) {
				t.Fatal("event was not consumed")
			}
			if tc.want == "" {
				if tab.toast.text != "" {
					t.Fatal("protocol internals leaked to toast")
				}
			} else if !strings.Contains(tab.toast.text, tc.want) {
				t.Fatalf("toast: %+v", tab.toast)
			}
			if tab.extensionEvent("ordinary notice") {
				t.Fatal("ordinary notice was consumed")
			}
		})
	}
}
