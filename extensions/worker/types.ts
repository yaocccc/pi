


export type WorkerMode = "scout" | "implement" | "test" | "review" | "fix";
export type WorkerPreset = "auto" | "fast" | "normal" | "deep" | "max";
export type Thinking = "high" | "xhigh" | "max";
export type ResolvedPreset = Exclude<WorkerPreset, "auto">;

export interface WorkerTask {
	mode: WorkerMode;
	objective: string;
	preset?: WorkerPreset;
	/** Must be true when `preset: max` reflects an explicit user request. */
	userExplicitMax?: boolean;
	context?: string;
	relevantFiles?: string[];
	allowedPaths?: string[];
	forbiddenPaths?: string[];
	acceptanceCriteria?: string[];
	verificationCommands?: string[];
	outputRequirements?: string[];
	cwd?: string;
}

export interface WorkerToolInput {
	task?: WorkerTask;
	tasks?: WorkerTask[];
	/** Required for calls made while automatic delegation is disabled in routing config. */
	manual?: boolean;
}

export interface PresetConfig {
	model: string;
	thinking: Thinking;
}

export type RoutingConfig = Record<ResolvedPreset, PresetConfig> & {
	version: number;
	maxConcurrentWorkers: number;
	automaticDelegationEnabled: boolean;
	defaultTimeoutMs: number;
	maxOutputBytes: number;
};

export interface Route {
	requestedPreset: WorkerPreset;
	resolvedPreset: ResolvedPreset;
	modelId: string;
	provider: string;
	thinking: Thinking;
	routeReason: string[];
}

export interface CommandResult {
	code: number;
	stdout: Buffer;
	stderr: Buffer;
}

export interface WorkspaceSnapshot {
	gitRoot: string;
	cwd: string;
	statusPaths: Set<string>;
	files: Map<string, { worktree: string | null; index: string | null }>;
	fsEntries?: Map<string, string>;
	excludeAgentRuntime: boolean;
	externalSymlinks: string[];
}

export type WorkerUiStatus = "queued" | "running" | "completed" | "blocked" | "failed";
export type WorkerUiActivityStatus = "running" | "completed" | "failed";

export interface WorkerUsage {
	input: number;
	output: number;
	cacheRead: number;
	cacheWrite: number;
	contextTokens: number;
	turns: number;
}

export interface WorkerUiActivity {
	id: string;
	type: "phase" | "tool" | "thinking";
	status: WorkerUiActivityStatus;
	label: string;
	detail?: string;
	at: number;
}

export interface ChildProgress {
	phase: string;
	activities: WorkerUiActivity[];
	toolCalls: number;
	usage: WorkerUsage;
	actualModel?: string;
}

export interface WorkerUiTask {
	index: number;
	mode: WorkerMode;
	objective: string;
	status: WorkerUiStatus;
	requestedPreset: WorkerPreset;
	resolvedPreset?: ResolvedPreset;
	modelId?: string;
	thinking?: Thinking;
	attempt: number;
	phase: string;
	startedAt?: number;
	finishedAt?: number;
	activities: WorkerUiActivity[];
	toolCalls: number;
	usage: WorkerUsage;
	result?: Record<string, any>;
}

export interface WorkerUiDetails {
	kind: "worker-ui";
	startedAt: number;
	finishedAt?: number;
	limit: number;
	total: number;
	completed: number;
	tasks: WorkerUiTask[];
	payload?: Record<string, any>;
}

export interface ChildResult {
	exitCode: number;
	stderr: string;
	assistantText: string;
	actualProvider?: string;
	actualModel?: string;
	stopReason?: string;
	errorMessage?: string;
	aborted: boolean;
	timedOut: boolean;
	truncated: boolean;
	activities: WorkerUiActivity[];
	toolCalls: number;
	usage: WorkerUsage;
}
