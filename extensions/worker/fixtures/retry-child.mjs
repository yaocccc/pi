// Relevant JSON mode events from AgentSession's retry cycle (no provider calls).
const emit = event => console.log(JSON.stringify(event));
const failed = process.argv.at(-1).includes("exhaust-retries");
const error = { role: "assistant", content: [], stopReason: "error", errorMessage: "429 rate limit" };
emit({ type: "message_end", message: error });
emit({ type: "agent_end", messages: [error], willRetry: true });
emit({ type: "auto_retry_start", attempt: 1, maxAttempts: 1, delayMs: 1, errorMessage: error.errorMessage });
const result = failed ? error : { role: "assistant", content: [{ type: "text", text: '{"status":"completed"}' }], stopReason: "stop" };
emit({ type: "message_end", message: result });
if (!failed) emit({ type: "auto_retry_end", success: true, attempt: 1 });
emit({ type: "agent_end", messages: [result], willRetry: false });
if (failed) emit({ type: "auto_retry_end", success: false, attempt: 1, finalError: error.errorMessage });
emit({ type: "agent_settled", aborted: false });
process.exitCode = failed ? 1 : 0;
