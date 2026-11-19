import { StringDecoder } from "node:string_decoder";

/** Incremental UTF-8 framing for a Worker's NDJSON stdout. Never split or
 * measure the entire unfinished line again when another pipe chunk arrives. */
export class WorkerEventLines {
	private decoder = new StringDecoder("utf8");
	private tail = "";
	private tailBytes = 0;

	constructor(private maxBytes: number) {}

	write(chunk: Buffer, onLine: (line: string) => void): boolean {
		const lines = this.decoder.write(chunk).split("\n");
		const suffix = lines.pop()!;
		if (lines.length) {
			lines[0] = this.tail + lines[0];
			this.tail = "";
			this.tailBytes = 0;
		}
		this.tailBytes += Buffer.byteLength(suffix);
		if (this.tailBytes > this.maxBytes) {
			this.tail = "";
			this.tailBytes = 0;
			return false;
		}
		this.tail += suffix;
		for (const line of lines) {
			if (!line.trim()) continue;
			if (Buffer.byteLength(line) > this.maxBytes) return false;
			onLine(line);
		}
		return true;
	}

	end(onLine: (line: string) => void): void {
		const last = this.tail + this.decoder.end();
		this.tail = "";
		this.tailBytes = 0;
		if (last.trim()) onLine(last);
	}
}
