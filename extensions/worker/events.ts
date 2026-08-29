export const WORKER_USAGE_EVENT = "worker:usage";

export interface WorkerUsageSnapshot {
	taskId: string;
	input: number;
	output: number;
}
