


export type WorkerMode = "scout" | "implement" | "test" | "review" | "fix";
export type WorkerSessionMode = "new" | "fork";
export type WorkerPreset = "fast" | "normal" | "deep";
export type Thinking = "off" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max";

export interface WorkerTask {
	mode: WorkerMode;
	objective: string;
	preset: WorkerPreset;
	/** Explicit fork snapshots the parent's active branch at batch admission. */
	sessionMode: WorkerSessionMode;
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
	tasks?: WorkerTask[];
	batchId?: string;
	cancel?: true;
}

export interface PresetConfig {
	model: string;
	thinking: Thinking;
}

export type RoutingConfig = Record<WorkerPreset, PresetConfig> & {
	version: number;
	maxConcurrentWorkers: number;
	defaultTimeoutMs: number;
	maxOutputBytes: number;
};

export interface Route {
	requestedPreset: WorkerPreset;
	resolvedPreset: WorkerPreset;
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
	/** Historical argument + result token estimate, independent of the UI preview. */
	contextSizeEstimate?: number;
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
	sessionMode?: WorkerSessionMode;
	resolvedPreset?: WorkerPreset;
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
	batchId?: string;
	originToolCallId?: string;
	revision?: number;
	controlErrors?: Array<{ toolCallId: string; message: string }>;
	/** Capture time for static history; elapsed times must not keep advancing. */
	snapshotAt?: number;
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
	termination?: { stage: "process" | "spawn" | "slot"; code: string; source: string; parentPid: number; workerPid?: number; ownerId: string };
	aborted: boolean;
	timedOut: boolean;
	truncated: boolean;
	activities: WorkerUiActivity[];
	toolCalls: number;
	usage: WorkerUsage;
}
