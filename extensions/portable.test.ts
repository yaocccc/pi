import assert from "node:assert/strict";
import { cpSync, mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawn, spawnSync } from "node:child_process";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = fileURLToPath(new URL("./", import.meta.url));
const cli = join(dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent"))), "cli.js");

test("local package installs and exposes RPC from an unrelated user directory; vanilla Pi remains usable", { timeout: 45_000 }, async () => {
	const home = mkdtempSync(join(tmpdir(), "pi-portable-"));
	const pkg = join(home, "checkout", "extensions"), agent = join(home, "user", "agent"), cwd = join(home, "project");
	mkdirSync(cwd, { recursive: true });
	cpSync(root, pkg, { recursive: true, filter: source => !source.split("/").includes("node_modules") });
	// Represents npm ci without contacting the network or duplicating installed dependencies.
	symlinkSync(join(root, "node_modules"), join(pkg, "node_modules"), "dir");
	const env: NodeJS.ProcessEnv = {};
	for (const [name, value] of Object.entries(process.env)) {
		if (!name.startsWith("PI_") && !name.startsWith("HERDR_") && !/API_KEY|TOKEN|SECRET|PASSWORD/.test(name)) env[name] = value;
	}
	Object.assign(env, { HOME: home, XDG_CONFIG_HOME: join(home, ".config"), PI_CODING_AGENT_DIR: agent });
	try {
		const installed = spawnSync(process.execPath, [cli, "install", pkg], { cwd, env, encoding: "utf8", timeout: 20_000 });
		assert.equal(installed.status, 0, installed.stderr || installed.stdout);
		const settings = JSON.parse(readFileSync(join(agent, "settings.json"), "utf8"));
		assert.ok(settings.packages.some((source: string) => resolve(agent, source) === pkg), JSON.stringify(settings));
		for (const enabled of [true, false]) {
			const child = spawn(process.execPath, [cli, "--mode", "rpc", "--no-session", ...(enabled ? [] : ["--no-extensions"])], { cwd, env, stdio: "pipe" });
			let stderr = "";
			child.stderr.on("data", chunk => { stderr += chunk; });
			const lines: any[] = [], waiting: Array<() => void> = [];
			const reader = createInterface({ input: child.stdout });
			reader.on("line", line => { try { lines.push(JSON.parse(line)); waiting.splice(0).forEach(resolve => resolve()); } catch {} });
			const wait = async (predicate: (line: any) => boolean) => {
				const deadline = Date.now() + 12_000;
				while (!lines.some(predicate)) {
					assert.ok(Date.now() < deadline && child.exitCode === null, `RPC response missing: ${stderr}`);
					await new Promise<void>(resolve => { const timer = setTimeout(resolve, 100); waiting.push(() => { clearTimeout(timer); resolve(); }); });
				}
				return lines.find(predicate);
			};
			try {
				child.stdin.write(JSON.stringify({ id: "commands", type: "get_commands" }) + "\n");
				const commands = await wait(line => line.type === "response" && line.id === "commands");
				assert.equal(commands.success, true);
				assert.equal(commands.data.commands.some((command: any) => command.name === "pi-ui-rpc" && command.source === "extension"), enabled);
				if (enabled) {
					child.stdin.write(JSON.stringify({ id: "request", type: "prompt", message: '/pi-ui-rpc {"id":"portable","method":"codex-fast.get","params":{}}' }) + "\n");
					const event = await wait(line => line.type === "extension_ui_request" && line.method === "notify" && line.message?.startsWith("pi-ui:rpc:v1:"));
					const response = JSON.parse(event.message.slice("pi-ui:rpc:v1:".length));
					assert.equal(response.id, "portable");
					assert.equal(response.result.level, "normal");
					assert.deepEqual(response.result.levels, ["normal", "fast", "ultrafast"]);
					const accepted = await wait(line => line.type === "response" && line.id === "request");
					assert.equal(accepted.data.disposition, "handled");
					assert.ok(!lines.some(line => line.type === "agent_start"), "extension RPC must never start an LLM turn");
				}
			} finally {
				reader.close(); child.kill("SIGTERM");
				await new Promise<void>(resolve => { if (child.exitCode !== null || child.signalCode !== null) resolve(); else child.once("exit", () => resolve()); });
			}
		}
	} finally { rmSync(home, { recursive: true, force: true }); }
});
