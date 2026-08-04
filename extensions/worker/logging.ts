import { randomUUID } from "node:crypto";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

export const WORKER_LOG_DIR = path.join(os.homedir(), ".pi", "logs");

export function logWorkerReturn(toolCallId: string, content: string): void {
	try {
		fs.mkdirSync(WORKER_LOG_DIR, { recursive: true, mode: 0o700 });
		fs.chmodSync(WORKER_LOG_DIR, 0o700);
		const timestamp = new Date().toISOString();
		const filename = `worker-${timestamp.replace(/[:.]/g, "-")}-${process.pid}-${randomUUID()}.json`;
		// TODO: Remove this temporary Worker return logging after output debugging is complete.
		fs.writeFileSync(path.join(WORKER_LOG_DIR, filename), `${JSON.stringify({ timestamp, toolCallId, content }, null, 2)}\n`, { encoding: "utf8", mode: 0o600, flag: "wx" });
	} catch {
		// Logging is best-effort and must never change the Worker result.
	}
}
