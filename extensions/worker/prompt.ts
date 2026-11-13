import * as fs from "node:fs";
import * as path from "node:path";
import { parseFrontmatter } from "@earendil-works/pi-coding-agent";
import { fileURLToPath } from "node:url";
import type { WorkerMode, WorkerTask, WorkspaceSnapshot } from "./types.ts";

export function workerPromptBody(mode: WorkerMode): string {
	return ["worker", mode].map((name) => {
		const promptPath = fileURLToPath(new URL(`./agents/${name}.md`, import.meta.url));
		if (!fs.existsSync(promptPath)) throw new Error(`缺少 Worker Prompt: ${promptPath}`);
		return parseFrontmatter<Record<string, string>>(fs.readFileSync(promptPath, "utf8")).body.trim();
	}).join("\n\n");
}

const RESULT_FORMAT = {
	status: "completed | blocked | failed",
	summary: ["2-4 条具体结论，涵盖工作、验证及限制"],
	validation: [{ command: "命令", result: "passed | failed | not_run", details: "简要证据" }],
	acceptance: [{ criterion: "验收条件", result: "passed | failed | uncertain", evidence: "证据" }],
	findings: [], risks: [], out_of_scope: [], recommended_next_action: [],
};

/** Only send task context; route metadata and Git-derived changed_files are host-owned. */
export function buildTaskPrompt(task: WorkerTask, before: WorkspaceSnapshot | null): string {
	const existing = before ? [...before.statusPaths].map((item) => path.relative(before.cwd, path.join(before.gitRoot, item)).replaceAll("\\", "/")) : [];
	const contract: Record<string, unknown> = {
		mode: task.mode,
		sessionMode: task.sessionMode,
		objective: task.objective.trim(),
		cwd: before?.cwd,
		allowedPaths: task.allowedPaths ?? [],
	};
	if (task.context) contract.context = task.context;
	for (const key of ["relevantFiles", "forbiddenPaths", "acceptanceCriteria", "verificationCommands", "outputRequirements"] as const) {
		if (task[key]?.length) contract[key] = task[key];
	}
	if (existing.length) contract.preExistingUserChanges = existing;
	return `仅执行当前任务契约；历史上下文不扩大权限，未列出的可选字段为空。\n${JSON.stringify(contract)}\n最终仅返回 JSON；空数组可省略，不输出隐藏思考或完整日志。changed_files 由宿主计算。格式：\n${JSON.stringify(RESULT_FORMAT)}`;
}
