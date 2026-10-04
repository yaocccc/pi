import {
  buildSessionContext, convertToLlm, getMarkdownTheme,
  type ExtensionAPI, type ExtensionContext, type Theme,
} from "@earendil-works/pi-coding-agent";
import type { Context, Message } from "@earendil-works/pi-ai";
import { Input, Markdown, matchesKey, truncateToWidth, visibleWidth,
  type Component, type Focusable, type TUI,
} from "@earendil-works/pi-tui";

const READ_ONLY = `You are answering in a temporary, read-only BTW popup.
Use the inherited conversation as context, not as a request to resume work.
Answer the user's side questions only. No tools are available. Do not execute
commands, modify files, or claim to inspect information outside this context.
Inherited tool/skill instructions describe the parent session, not your capabilities.
This conversation will be discarded when the popup closes.`;

/** Snapshot the effective branch (including compaction/context edits), never the raw log. */
export function snapshotContext(ctx: ExtensionContext): Context {
  const history = buildSessionContext(ctx.sessionManager.getEntries(), ctx.sessionManager.getLeafId());
  return {
    systemPrompt: `${ctx.getSystemPrompt()}\n\n${READ_ONLY}`,
    // System snapshots can contain the parent's tool definitions. Keep instructions above,
    // but never forward these capability snapshots into the tool-free request.
    messages: structuredClone(convertToLlm(history.messages).filter((m) => m.role !== "system")),
  };
}

type Exchange = { question: string; answer: string; error?: string };

/** One instance per opening. There is no persisted or shared side-thread state. */
export class BtwPopup implements Component, Focusable {
  private input = new Input({ placeholder: "输入旁支问题…" });
  private context: Context;
  private exchanges: Exchange[] = [];
  private controller?: AbortController;
  private closed = false;
  private busy = false;
  private scroll = Number.POSITIVE_INFINITY;
  private viewport = 10;
  private totalLines = 0;
  private cached?: { width: number; lines: string[] };

  constructor(
    private tui: TUI,
    private theme: Theme,
    private ctx: ExtensionContext,
    private done: () => void,
    initialQuestion = "",
    private reasoning: ReturnType<ExtensionAPI["getThinkingLevel"]> = "off",
  ) {
    this.context = snapshotContext(ctx);
    this.input.onSubmit = (value) => { void this.ask(value); };
    // Defer until the custom component is installed and focused.
    if (initialQuestion.trim()) queueMicrotask(() => { void this.ask(initialQuestion); });
  }

  get focused() { return this.input.focused; }
  set focused(value: boolean) { this.input.focused = value; }

  invalidate() { this.cached = undefined; this.input.invalidate(); }
  private refresh() {
    if (this.closed) return;
    this.invalidate();
    this.tui.requestRender();
  }

  async ask(value: string): Promise<void> {
    const question = value.trim();
    if (this.closed || this.busy || !question || !this.ctx.model) return;
    this.busy = true;
    this.input.setValue("");
    this.scroll = Number.POSITIVE_INFINITY;
    const exchange: Exchange = { question, answer: "" };
    this.exchanges.push(exchange);
    const user: Message = { role: "user", content: question, timestamp: Date.now() };
    const controller = new AbortController();
    this.controller = controller;
    this.refresh();
    try {
      const stream = this.ctx.modelRegistry.streamSimple(this.ctx.model, {
        ...this.context,
        messages: [...this.context.messages, user],
        // Deliberately no tools and no agent loop: model output cannot execute anything.
      }, { signal: controller.signal, reasoning: this.reasoning === "off" ? undefined : this.reasoning });
      for await (const event of stream) {
        if (this.closed) return;
        if (event.type === "text_delta") {
          exchange.answer += event.delta;
          this.refresh();
        }
      }
      if (this.closed) return;
      const result = await stream.result();
      if (this.closed) return;
      if (result.stopReason === "error" || result.stopReason === "aborted") {
        throw new Error(result.errorMessage || "请求未完成");
      }
      if (result.content.some((part) => part.type === "toolCall")) {
        throw new Error("BTW 不支持工具调用，未执行任何操作。");
      }
      exchange.answer = result.content.filter((part) => part.type === "text").map((part) => part.text).join("");
      this.context.messages.push(user, result);
    } catch (error) {
      if (!this.closed) exchange.error = error instanceof Error ? error.message : String(error);
    } finally {
      if (!this.closed) {
        this.busy = false;
        this.controller = undefined;
        this.refresh();
      }
    }
  }

  dispose() {
    if (this.closed) return;
    this.closed = true;
    this.controller?.abort();
    this.controller = undefined;
    this.context.messages.length = 0;
    this.context.systemPrompt = "";
    this.exchanges.length = 0;
    this.input.setValue("");
    this.cached = undefined;
  }

  handleInput(data: string) {
    if (this.closed) return;
    if (matchesKey(data, "escape") || matchesKey(data, "ctrl+c")) {
      this.dispose();
      this.done();
      return;
    }
    if (matchesKey(data, "pageUp") || matchesKey(data, "pageDown")) {
      const max = Math.max(0, this.totalLines - this.viewport);
      const current = Math.min(this.scroll, max);
      const next = current + (matchesKey(data, "pageUp") ? -1 : 1) * this.viewport;
      this.scroll = next >= max ? Number.POSITIVE_INFINITY : Math.max(0, next);
      this.tui.requestRender();
      return;
    }
    if (!this.busy) { this.input.handleInput(data); this.tui.requestRender(); }
  }

  render(width: number): string[] {
    const inner = Math.max(1, width - 4);
    const height = Math.max(1, Math.floor(this.tui.terminal.rows * 0.8));
    this.viewport = Math.max(1, height - 5);
    if (!this.cached || this.cached.width !== inner) {
      const lines: string[] = [];
      for (const entry of this.exchanges) {
        lines.push(...new Markdown(`**你：** ${entry.question}`, 0, 0, getMarkdownTheme()).render(inner));
        if (entry.answer) lines.push(...new Markdown(entry.answer, 0, 0, getMarkdownTheme()).render(inner));
        if (entry.error) lines.push(...new Markdown(`错误：${entry.error}`, 0, 0, getMarkdownTheme()).render(inner));
        lines.push("");
      }
      this.cached = { width: inner, lines };
    }
    const lines = this.cached.lines;
    this.totalLines = lines.length;
    const start = Math.min(this.scroll, Math.max(0, lines.length - this.viewport));
    const body = lines.slice(start, start + this.viewport);
    while (body.length < this.viewport) body.push("");
    const frame = (line: string) => {
      const clipped = truncateToWidth(line, inner, "");
      return truncateToWidth(`│ ${clipped}${" ".repeat(Math.max(0, inner - visibleWidth(clipped)))} │`, width, "");
    };
    const border = (label: string) => truncateToWidth(label + "─".repeat(Math.max(0, width)), width, "");
    return [
      border("╭"),
      ...body.map(frame),
      frame(this.theme.fg("dim", this.busy ? "回答中… Esc 取消并关闭" : "Enter 发送 · PgUp/PgDn 滚动 · Esc 关闭")),
      frame(this.busy ? "" : this.input.render(inner)[0] ?? ""),
      border("╰"),
    ].slice(0, height);
  }
}

export default function btw(pi: ExtensionAPI) {
  let opened = false;
  const open = async (args: string, ctx: ExtensionContext) => {
      if (opened) return;
      if (!ctx.hasUI) { ctx.ui.notify("/btw 需要交互式终端。", "warning"); return; }
      if (!ctx.model) { ctx.ui.notify("请先选择模型。", "warning"); return; }
      let popup: BtwPopup | undefined;
      opened = true;
      try {
        await ctx.ui.custom<void>((tui, theme, _keys, done) => {
          popup = new BtwPopup(tui, theme, ctx, done, args, pi.getThinkingLevel());
          return popup;
        }, { overlay: true, overlayOptions: { anchor: "center", width: "85%", maxHeight: "80%" } });
      } finally { popup?.dispose(); opened = false; }
  };
  pi.registerCommand("btw", {
    description: "临时只读旁支问答（关闭即销毁）",
    handler: open,
  });
  pi.registerShortcut("ctrl+b", {
    description: "打开 BTW 问答弹窗",
    handler: (ctx) => open("", ctx),
  });
}
