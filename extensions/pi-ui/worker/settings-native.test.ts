import assert from "node:assert/strict";
import * as fs from "node:fs";
import test from "node:test";
import { supportedThinking } from "../../worker/config.ts";
import { THINKING_LEVELS } from "../../worker/api.ts";
import { saveWorkerSettings } from "../../worker/settings.ts";
import { context, settings, settingsFile } from "../../worker/test-helpers.ts";
import { configureWorkerSettings } from "./settings-ui.ts";

test("settings UI keeps thinking on model changes and requires an explicit compatible choice", async (t) => {
	const file = settingsFile(t);
	const current = settings();
	current.fast.thinking = "medium";
	const original = JSON.stringify(current);
	fs.writeFileSync(file, original);
	const choices = ["Fast ·", "Thinking · medium", undefined, "模型 ·", "fixture/plain", "返回", "保存并退出", "Fast ·", "Thinking · medium", "off", "返回", "保存并退出"];
	const notifications: string[] = [];
	let step = 0, thinkingMenus = 0;
	const ctx = {
		modelRegistry: { getAvailable: () => [...context().modelRegistry.getAvailable(), { provider: "fixture", id: "plain", reasoning: false }] },
		ui: {
			select: async (title: string, options: string[]) => {
				assert.ok(step < choices.length, "unexpected settings loop");
				assert.ok(options.every((option) => !option.includes("自动委派")));
				if (title === "Fast Thinking") {
					const levels = options.map((option) => option.slice(2));
					assert.deepEqual(levels, thinkingMenus++ === 0 ? [...THINKING_LEVELS] : ["off"]);
				}
				if (step === 7) {
					assert.equal(fs.readFileSync(file, "utf8"), original, "incompatible settings were not saved");
					assert.ok(notifications.some((notice) => notice.includes("不支持 medium")));
				}
				const choice = choices[step++];
				if (choice === undefined) return undefined;
				const selected = options.find((option) => option.includes(choice));
				assert.ok(selected, `missing option ${choice}: ${options.join(", ")}`);
				return selected;
			},
			notify: (message: string) => notifications.push(message),
		},
	} as any;
	const result = await configureWorkerSettings(ctx, current, (value) => saveWorkerSettings(ctx, value, file), () => ctx.modelRegistry.getAvailable().map((model: any) => ({ id: `${model.provider}/${model.id}`, thinking: supportedThinking(model) })));
	assert.equal(step, choices.length);
	assert.equal(thinkingMenus, 2);
	assert.ok(notifications.some((notice) => notice.includes("已保留原值")));
	assert.equal(JSON.stringify(current), original, "settings UI must not mutate input");
	assert.deepEqual(result?.fast, { model: "fixture/plain", thinking: "off" });
	assert.deepEqual(JSON.parse(fs.readFileSync(file, "utf8")), result);
});
