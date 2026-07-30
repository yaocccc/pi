import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";
import { parseFrontmatter } from "@earendil-works/pi-coding-agent";
import {
	AGENT_NAMES,
	AGENT_THINKING_LEVELS,
	type AgentDefinition,
	type AgentName,
	type AgentThinkingLevel,
} from "./types/index.ts";

const AGENTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), "agents");

function isAgentName(value: string): value is AgentName {
	return AGENT_NAMES.includes(value as AgentName);
}

function parseThinking(value: unknown, filePath: string): AgentThinkingLevel {
	if (typeof value !== "string" || !AGENT_THINKING_LEVELS.includes(value as AgentThinkingLevel)) {
		throw new Error(`Agent thinking 无效: ${filePath}`);
	}
	return value as AgentThinkingLevel;
}

function parseExcludeTools(value: unknown, filePath: string): string[] {
	if (value === undefined) return [];
	if (typeof value !== "string") {
		throw new Error(`Agent excludeTools 无效: ${filePath}`);
	}
	return [
		...new Set(
			value
				.split(",")
				.map((tool) => tool.trim())
				.filter(Boolean),
		),
	];
}

export function loadAgents(): Map<AgentName, AgentDefinition> {
	const agents = new Map<AgentName, AgentDefinition>();

	for (const expectedName of AGENT_NAMES) {
		const filePath = path.join(AGENTS_DIR, `${expectedName}.md`);
		const content = fs.readFileSync(filePath, "utf8");
		const { frontmatter, body } = parseFrontmatter<Record<string, unknown>>(content);
		const rawName = frontmatter.name;
		const description = frontmatter.description;

		if (typeof rawName !== "string" || !isAgentName(rawName) || rawName !== expectedName) {
			throw new Error(`Agent 名称无效: ${filePath}`);
		}
		if (typeof description !== "string" || !description.trim()) {
			throw new Error(`Agent 描述无效: ${filePath}`);
		}
		if (!body.trim()) throw new Error(`Agent 提示词不能为空: ${filePath}`);

		const excludeTools = parseExcludeTools(frontmatter.excludeTools, filePath);
		if (rawName !== "worker") {
			const missingWriteTools = ["edit", "write"].filter((tool) => !excludeTools.includes(tool));
			if (missingWriteTools.length) {
				throw new Error(`Agent ${rawName} 必须通过 excludeTools 排除写工具: ${missingWriteTools.join(", ")} (${filePath})`);
			}
		}

		agents.set(rawName, {
			name: rawName,
			description: description.trim(),
			thinking: parseThinking(frontmatter.thinking, filePath),
			excludeTools,
			systemPrompt: body.trim(),
			filePath,
		});
	}

	return agents;
}
