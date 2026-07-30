import type { Message } from "@earendil-works/pi-ai";

export const AGENT_NAMES = ["scout", "planner", "worker", "reviewer"] as const;

export type AgentName = (typeof AGENT_NAMES)[number];
export type RunMode = "single" | "chain";
export type RunStatus = "running" | "completed" | "failed";

export interface AgentDefinition {
	name: AgentName;
	description: string;
	tools: string[];
	systemPrompt: string;
	filePath: string;
}

export interface UsageStats {
	input: number;
	output: number;
	cacheRead: number;
	cacheWrite: number;
	cost: number;
	contextTokens: number;
	turns: number;
}

export type ToolActivityStatus = "running" | "completed" | "failed";

export interface ToolActivity {
	id: string;
	name: string;
	status: ToolActivityStatus;
	input?: string;
	output?: string;
}

export interface AgentRunResult {
	agent: AgentName;
	task: string;
	status: RunStatus;
	exitCode: number | null;
	messages: Message[];
	toolActivities: ToolActivity[];
	toolCalls: number;
	latestThinking?: string;
	stderr: string;
	usage: UsageStats;
	model?: string;
	thinking?: string;
	stopReason?: string;
	errorMessage?: string;
	step?: number;
}

export interface SubagentDetails {
	mode: RunMode;
	results: AgentRunResult[];
	totalSteps: number;
	model?: string;
	thinking?: string;
}

export interface ChainTask {
	agent: AgentName;
	task: string;
}

export interface SubagentToolParams {
	agent?: AgentName;
	task?: string;
	chain?: ChainTask[];
}
