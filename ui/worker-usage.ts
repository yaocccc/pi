import type { WorkerUsageSnapshot } from "../worker/events.ts";
import type { TokenUsage } from "./types.ts";

export const combineTokenUsage = (main: TokenUsage, workers: TokenUsage): Required<TokenUsage> => ({
    input: (main.input ?? 0) + (workers.input ?? 0),
    output: (main.output ?? 0) + (workers.output ?? 0),
});

export const calculateTps = (usage: TokenUsage, elapsedSeconds: number): number | undefined =>
    elapsedSeconds > 0 && (usage.output ?? 0) > 0 ? (usage.output ?? 0) / elapsedSeconds : undefined;

export class WorkerUsageTracker {
    private readonly snapshots = new Map<string, TokenUsage>();

    reset(): void {
        this.snapshots.clear();
    }

    update(value: unknown): boolean {
        if (!value || typeof value !== "object") return false;
        const { taskId, input, output } = value as Partial<WorkerUsageSnapshot>;
        if (typeof taskId !== "string" || !taskId || typeof input !== "number" || !Number.isFinite(input) || typeof output !== "number" || !Number.isFinite(output)) return false;
        this.snapshots.set(taskId, { input: Math.max(0, input), output: Math.max(0, output) });
        return true;
    }

    total(): Required<TokenUsage> {
        let input = 0;
        let output = 0;
        for (const usage of this.snapshots.values()) {
            input += usage.input ?? 0;
            output += usage.output ?? 0;
        }
        return { input, output };
    }
}
