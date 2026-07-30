import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";
import { parseFrontmatter } from "@earendil-works/pi-coding-agent";
import { AGENT_NAMES, type AgentDefinition, type AgentName } from "./types/index.ts";

const AGENTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), "agents");

function isAgentName(value: string): value is AgentName {
	return AGENT_NAMES.includes(value as AgentName);
}

function parseTools(value: unknown, filePath: string): string[] {
	if (typeof value !== "string") {
		throw new Error(`Agent 配置缺少 tools: ${filePath}`);
	}
	const tools = value
		.split(",")
		.map((tool) => tool.trim())
		.filter(Boolean);
	if (tools.length === 0) throw new Error(`Agent tools 不能为空: ${filePath}`);
	return tools;
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

		agents.set(rawName, {
			name: rawName,
			description: description.trim(),
			tools: parseTools(frontmatter.tools, filePath),
			systemPrompt: body.trim(),
			filePath,
		});
	}

	return agents;
}
