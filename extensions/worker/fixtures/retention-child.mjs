import { holdTranscript, cleanupTranscripts } from "../transcript-retention.ts";

const [mode, root, idOrTime, budget] = process.argv.slice(2);
if (mode === "cleanup") {
 await cleanupTranscripts(root, { now: Number(idOrTime), maxBytes: Number(budget) });
 console.log("done");
} else {
 const release = holdTranscript(root, idOrTime);
 console.log("ready");
 process.stdin.resume();
 process.stdin.on("end", () => {
  if (mode !== "crash") release();
  // Simulate a process that dies before removing its lease without killing any
  // real Worker or using a model. The parent waits for actual exit before GC.
 });
}
