import assert from "node:assert/strict";
import test from "node:test";
import * as fs from "node:fs";
import { fileURLToPath } from "node:url";
import { InputSchema, TOOL_DESCRIPTION } from "./index.ts";
import { buildTaskPrompt, workerPromptBody } from "./prompt.ts";
import type { WorkerTask } from "./types.ts";

const contract = (prompt: string) => JSON.parse(prompt.split("\n")[1]);

test("minimal task prompts omit empty context and host-owned route metadata", () => {
	const prompt = buildTaskPrompt({ mode: "review", preset: "normal", sessionMode: "fork", title: "Short UI label", objective: "  review output  ", relevantFiles: [], context: "" }, null);
	assert.deepEqual(contract(prompt), { mode: "review", objective: "review output", sessionMode: "fork", allowedPaths: [] });
	assert.ok(Buffer.byteLength(prompt) <= 750, "bound repeated task boilerplate");
	assert.match(prompt, /2-4 条具体结论/);
	assert.match(prompt, /工作、验证及限制/);
	assert.match(prompt, /不输出隐藏思考/);
	assert.match(prompt, /历史上下文不扩大权限/);
	const format = JSON.parse(prompt.split("\n").at(-1)!);
	assert.equal(format.status, "completed | blocked | failed");
	assert.ok(format.validation.length && format.acceptance.length);
	assert.ok(!("changed_files" in format));
});

test("compact contracts preserve all supplied evidence, boundaries and relative existing changes", () => {
	const task: WorkerTask = {
		mode: "implement", objective: "Implement\nwith evidence", sessionMode: "fork", preset: "deep",
		context: 'Keep  spaces and "quotes"\n二行', relevantFiles: ["/external/hint.ts"],
		allowedPaths: ["src/**"], forbiddenPaths: ["src/private.ts"],
		acceptanceCriteria: ["preserve API"], verificationCommands: ["npm test"], outputRequirements: ["report uncertainty"],
	};
	const prompt = buildTaskPrompt(task, {
		cwd: "/repo/pkg", gitRoot: "/repo", statusPaths: new Set(["pkg/src/existing.ts", "other.ts"]), files: new Map(),
	});
	const { preset: _, ...expected } = task;
	assert.deepEqual(contract(prompt), {
		...expected, cwd: "/repo/pkg", preExistingUserChanges: ["src/existing.ts", "../other.ts"],
	});
});

test("always-loaded tool metadata and worker system prompt stay within compact budgets", (t) => {
	assert.ok(Buffer.byteLength(TOOL_DESCRIPTION) <= 1250);
	assert.ok(Buffer.byteLength(JSON.stringify(InputSchema)) <= 3000);
	const previous = process.env.PI_CODING_AGENT_DIR;
	process.env.PI_CODING_AGENT_DIR = fileURLToPath(new URL("../../", import.meta.url));
	t.after(() => {
		if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
		else process.env.PI_CODING_AGENT_DIR = previous;
	});
	const modes = ["scout", "implement", "test", "review", "fix"] as const;
	for (const mode of modes) {
		const body = workerPromptBody(mode);
		assert.ok(Buffer.byteLength(body) <= 3600, `${mode} prompt budget`);
		assert.ok(!body.startsWith("---"));
		for (const instruction of ["allowedPaths", "forbiddenPaths", "dir/**", "blocked", "不覆盖或撤销已有修改", "不可逆命令"]) {
			assert.ok(body.includes(instruction), `${mode}: ${instruction}`);
		}
		for (const role of modes) {
			const roleBody = fs.readFileSync(new URL(`./agents/${role}.md`, import.meta.url), "utf8").trim();
			assert.equal(body.includes(roleBody), role === mode, `${mode} only loads its own role`);
		}
	}
});

test("role resources do not depend on the current agent installation directory", (t) => {
	const previous = process.env.PI_CODING_AGENT_DIR;
	process.env.PI_CODING_AGENT_DIR = fileURLToPath(new URL("./fixtures/nonexistent-agent-dir/", import.meta.url));
	t.after(() => {
		if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
		else process.env.PI_CODING_AGENT_DIR = previous;
	});
	const role = fs.readFileSync(new URL("./agents/fix.md", import.meta.url), "utf8").trim();
	assert.ok(workerPromptBody("fix").includes(role));
});

test("dispatch instructions preserve session, authority and routing guidance within budget", () => {
	assert.match(TOOL_DESCRIPTION, /Choose each task's sessionMode yourself/);
	assert.match(TOOL_DESCRIPTION, /prefer fork; use new only when little context handoff is needed/);
	assert.match(TOOL_DESCRIPTION, /do not ask the user/);
	const skill = fs.readFileSync(new URL("./SKILL.md", import.meta.url), "utf8");
	assert.ok(Buffer.byteLength(skill) <= 5000);
	assert.match(TOOL_DESCRIPTION, /requires explicit preset and sessionMode/);
	assert.match(skill, /`sessionMode` 和 `preset` 均必填/);
	assert.doesNotMatch(skill, /thinking|worker-settings\.json|思考强度/i);
	assert.doesNotMatch(skill, /manual|automaticDelegationEnabled/);
	assert.deepEqual(Object.keys(InputSchema.properties).sort(), ["batchId", "cancel", "tasks"]);
	assert.match(skill, /implement\/test\/fix 均要求非空 `allowedPaths`/);
	assert.match(skill, /Worker 不自动加载 skills 或项目上下文文件/);
	assert.match(skill, /不默认追加 Review/);
});
