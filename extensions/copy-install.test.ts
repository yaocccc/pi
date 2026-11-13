import assert from "node:assert/strict";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { spawn, spawnSync } from "node:child_process";
import { createInterface } from "node:readline";
import test from "node:test";
import ts from "typescript";

const root = fileURLToPath(new URL("./", import.meta.url));
const modules = ["ask-question", "worker", "codex-fast", "sol-pi", "autoname", "filter-output"];
const cli = join(dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent"))), "cli.js");

test("bundled portable support matches its maintenance templates", () => {
	const result = spawnSync(process.execPath, [join(root, "scripts/sync-portable.mjs"), "--check"], { encoding: "utf8" });
	assert.equal(result.status, 0, result.stderr || result.stdout);
});

test("runtime imports stay inside each capability/UI copy pair", () => {
	for (const name of modules) {
		const allowed = [join(root, name), join(root, "pi-ui", name)];
		const seen = new Set<string>();
		const visit = (path: string) => {
			if (!existsSync(path)) path = [path + ".ts", path + ".mjs", path + ".js", join(path, "index.ts")].find(existsSync) ?? path;
			assert.ok(allowed.some(dir => path.startsWith(dir + "/")), `${name} escapes copy pair: ${path}`);
			assert.ok(existsSync(path), `Missing source ${path}`);
			if (seen.has(path)) return;
			seen.add(path);
			const source = ts.createSourceFile(path, readFileSync(path, "utf8"), ts.ScriptTarget.Latest, true);
			const walk = (node: ts.Node) => {
				let specifier: ts.Expression | undefined;
				if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) specifier = node.moduleSpecifier;
				else if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword || (ts.isIdentifier(node.expression) && node.expression.text === "require"))) specifier = node.arguments[0];
				if (specifier && ts.isStringLiteral(specifier) && specifier.text.startsWith(".")) visit(resolve(dirname(path), specifier.text));
				ts.forEachChild(node, walk);
			};
			walk(source);
		};
		for (const dir of allowed) visit(join(dir, "index.ts"));
	}
});

// No symlinks, node_modules or repository paths are available in the copied tree.
// Pi itself supplies its documented SDK/TUI/TypeBox imports.
for (const selected of [...modules.map(name => [name]), modules]) {
	test(`copy-only real Pi startup and explicit UI registration: ${selected.join(", ")}`, { timeout: 25_000 }, async () => {
		const home = mkdtempSync(join(tmpdir(), "pi-copy-pair-"));
		const target = join(home, "modules"), cwd = join(home, "project"), agent = join(home, "agent");
		mkdirSync(cwd, { recursive: true });
		mkdirSync(join(target, "ui"), { recursive: true });
		for (const name of selected) {
			for (const [source, destination] of [[join(root, name), join(target, name)], [join(root, "pi-ui", name), join(target, "ui", name)]]) {
				cpSync(source, destination, { recursive: true, filter: path => !path.split("/").includes("node_modules") });
			}
		}
		assert.equal(existsSync(join(target, "shared")), false);
		assert.equal(existsSync(join(target, "node_modules")), false);
		const registrations = selected.map((name, index) => `import ui${index} from "./ui/${name}/index.ts";`).join("\n");
		writeFileSync(join(target, "my-ui.ts"), `${registrations}\nexport default function(pi) { ${selected.map((_, i) => `ui${i}(pi);`).join(" ")} }\n`);
		// A test-only command inspects the loaded capability without a model turn.
		writeFileSync(join(target, "probe.ts"), `
import { getCapability } from "./${selected[0]}/capabilities.ts";
export default function(pi) {
 pi.registerCommand("copy-probe", { handler: async (_args, ctx) => {
  const services = {};
  for (const name of ["ask-question:v1", "worker:v1", "codex-fast"]) {
   const value = getCapability(pi, name); if (value) services[name] = value;
  }
  if (services["ask-question:v1"]) {
   const answer = await services["ask-question:v1"].ask({ questions: [{ question: "Choose", options: ["A", "B"] }], review: false, context: ctx });
   ctx.ui.notify("copy-answer:" + JSON.stringify(answer), "info");
  }
  for (const event of [
   { name: "autoname.renamed", data: { name: "Copied", contextTokens: 10, durationMs: 1 } },
   { name: "filter.redacted", data: { path: ".env" } },
   { name: "context.ready", data: { generationMs: 1 } },
   { name: "worker.settings-saved", data: {} }
  ]) pi.events.emit("pi:frontend-event:v1", { context: ctx, event });
  ctx.ui.notify("copy-probe:" + JSON.stringify({ services: Object.keys(services), tools: pi.getAllTools().map(t => t.name) }), "info");
 }});
}
`);
		const env: NodeJS.ProcessEnv = { HOME: home, USERPROFILE: home, PI_CODING_AGENT_DIR: agent };
		for (const key of ["PATH", "SystemRoot", "SYSTEMROOT", "COMSPEC", "PATHEXT", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "TERM"]) if (process.env[key] !== undefined) env[key] = process.env[key];
		const args = [cli, "--mode", "rpc", "--no-session", "--no-extensions", ...selected.flatMap(name => ["-e", join(target, name, "index.ts")]), "-e", join(target, "my-ui.ts"), "-e", join(target, "probe.ts")];
		const child = spawn(process.execPath, args, { cwd, env, stdio: "pipe" });
		let stderr = "", spawnError: Error | undefined;
		child.on("error", error => { spawnError = error; });
		child.stderr.on("data", chunk => { stderr += chunk; });
		const lines: any[] = [];
		const reader = createInterface({ input: child.stdout });
		reader.on("line", line => { try { lines.push(JSON.parse(line)); } catch {} });
		const send = (value: unknown) => child.stdin.write(JSON.stringify(value) + "\n");
		const wait = async (predicate: (value: any) => boolean) => {
			const deadline = Date.now() + 12_000;
			while (!lines.some(predicate)) {
				assert.ok(Date.now() < deadline && child.exitCode === null && !spawnError, `Missing copy RPC response: ${spawnError ?? stderr}\n${JSON.stringify(lines)}`);
				await new Promise(resolve => setTimeout(resolve, 20));
			}
			return lines.find(predicate);
		};
		try {
			send({ id: "commands", type: "get_commands" });
			const commands = (await wait(value => value.id === "commands" && value.type === "response")).data.commands;
			const bridge = selected.some(name => name === "worker" || name === "codex-fast");
			assert.equal(commands.filter((c: any) => c.name === "pi-ui-rpc" && c.source === "extension").length, bridge ? 1 : 0);
			send({ id: "probe", type: "prompt", message: "/copy-probe" });
			if (selected.includes("ask-question")) {
				const dialog = await wait(value => value.type === "extension_ui_request" && value.title === "pi-goui:ask-question:v1");
				send({ type: "extension_ui_response", id: dialog.id, value: JSON.stringify({ cancelled: false, answers: [{ question: "Choose", answers: ["A"], completed: true, customAnswers: [] }] }) });
				const answer = await wait(value => value.message?.startsWith("copy-answer:"));
				assert.equal(JSON.parse(answer.message.slice("copy-answer:".length)).cancelled, false);
			}
			const probe = JSON.parse((await wait(value => value.message?.startsWith("copy-probe:"))).message.slice("copy-probe:".length));
			for (const [name, key] of [["ask-question", "ask-question:v1"], ["worker", "worker:v1"], ["codex-fast", "codex-fast"]]) assert.equal(probe.services.includes(key), selected.includes(name));
			for (const [name, tool] of [["ask-question", "ask_question"], ["worker", "worker"], ["sol-pi", "update_plan"]]) assert.equal(probe.tools.includes(tool), selected.includes(name));
			for (const [name, event] of [["autoname", "autoname.renamed"], ["filter-output", "filter.redacted"], ["sol-pi", "context.ready"], ["worker", "worker.settings-saved"]]) {
				assert.equal(lines.filter(value => value.message?.startsWith("pi-goui:event:v1:") && JSON.parse(value.message.slice("pi-goui:event:v1:".length)).name === event).length, selected.includes(name) ? 1 : 0);
			}
			await wait(value => value.type === "response" && value.id === "probe");
			for (const [name, method, params] of [["codex-fast", "codex-fast.get", {}], ["worker", "worker.transcript", {}]] as const) {
				if (!selected.includes(name)) continue;
				send({ id: method, type: "prompt", message: "/pi-ui-rpc " + JSON.stringify({ id: method, method, params }) });
				const response = JSON.parse((await wait(value => value.message?.startsWith("pi-ui:rpc:v1:") && JSON.parse(value.message.slice("pi-ui:rpc:v1:".length)).id === method)).message.slice("pi-ui:rpc:v1:".length));
				if (name === "codex-fast") assert.equal(response.result.level, "normal");
				else assert.equal(response.error.code, "invalid-transcript");
				await wait(value => value.type === "response" && value.id === method);
			}
			assert.ok(!lines.some(value => value.type === "agent_start"), "copy integration must not start an LLM turn");
			assert.doesNotMatch(stderr, /Error loading extension|Failed to load extension|Cannot find module|ERR_MODULE_NOT_FOUND/i);
		} finally {
			reader.close(); child.kill("SIGTERM");
			await new Promise<void>(resolve => { if (child.exitCode !== null || child.signalCode !== null || spawnError) resolve(); else child.once("exit", () => resolve()); });
			rmSync(home, { recursive: true, force: true });
		}
	});
}
