// Offline integration harness: registers and executes the REAL local ask_question,
// with a real interactive Pi context. No provider or model call is made.
import type { ExtensionAPI, ToolDefinition } from "@earendil-works/pi-coding-agent";
import askQuestion from "../../extensions/ask-question/index.ts";

export default function fixture(pi: ExtensionAPI) {
    let question!: ToolDefinition;
    askQuestion({ ...pi, registerTool(tool) {
        if (tool.name === "ask_question") question = tool;
        pi.registerTool(tool);
    } });
    let serial = 0;
    pi.registerCommand("wb-real-question", {
        description: "Offline real ask_question integration",
        handler: async (args, ctx) => {
            const controller = new AbortController();
            const count = args === "queue" || args === "abort" ? 2 : 1;
            const timer = args === "rename" ? setTimeout(() => pi.setSessionName("等待期间异步命名 🧑‍💻"), 200)
                : args === "abort" ? setTimeout(() => controller.abort(new Error("offline cancellation")), 1000) : undefined;
            try {
                await Promise.allSettled(Array.from({ length: count }, (_, index) =>
                    question.execute("offline-" + (++serial), {
                        question: "真实提问 " + (index + 1),
                        options: ["选项甲", "选项乙"],
                    }, controller.signal, undefined, ctx)));
            } finally { if (timer) clearTimeout(timer); }
        },
    });
    pi.registerCommand("wb-async-name", {
        description: "Offline async setSessionName, same API as autoname",
        handler: async (args) => {
            await new Promise(resolve => setTimeout(resolve, 100));
            pi.setSessionName(args);
        },
    });
}
