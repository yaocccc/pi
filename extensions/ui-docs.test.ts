import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { validateQuestionResponse } from "./ask-question/validation.ts";
import { parseSettings } from "./worker/settings.ts";
import { WorkerTranscriptReader } from "./worker/transcript-reader.ts";

const guides = ["ask-question", "worker", "codex-fast", "sol-pi", "autoname", "filter-output", "telegram", "pi-ui"];
function examples(name: string, language: string): any[] {
	const source = readFileSync(new URL(`./${name}/README${language}.md`, import.meta.url), "utf8");
	return [...source.matchAll(/(?:~~~|```)json\s*\n([\s\S]*?)\n(?:~~~|```)/g)].map((match, index) => {
		try { return JSON.parse(match[1]); }
		catch (error) { throw new Error(`${name}/README${language}.md JSON example ${index + 1}: ${error}`); }
	});
}

for (const name of guides) test(`${name}: both standalone UI guides contain valid wire examples`, () => {
	for (const language of ["", "_EN"]) {
		const source = readFileSync(new URL(`./${name}/README${language}.md`, import.meta.url), "utf8");
		assert.match(source, /Third-party UI|第三方 UI/);
		assert.ok(source.split("\n").slice(0, 6).join("\n").includes(language ? "](README.md)" : "](README_EN.md)"), "language link must be at the top");
		const values = examples(name, language);
		for (const value of values) {
			if (value.type === "extension_ui_request" && value.placeholder?.startsWith("{")) JSON.parse(value.placeholder);
			if (value.type === "extension_ui_request" && value.message?.startsWith("pi-goui:event:v1:")) JSON.parse(value.message.slice("pi-goui:event:v1:".length));
			if (value.type === "prompt" && value.message?.startsWith("/pi-ui-rpc ")) JSON.parse(value.message.slice("/pi-ui-rpc ".length));
		}
	}
});

test("each copy-pair guide documents the same explicit UI registration layout", () => {
	for (const name of guides.slice(0, 6)) for (const directory of [name, `pi-ui/${name}`]) {
		for (const language of ["", "_EN"]) {
			const source = readFileSync(new URL(`./${directory}/README${language}.md`, import.meta.url), "utf8");
			assert.ok(source.split("\n").slice(0, 6).join("\n").includes(language ? "](README.md)" : "](README_EN.md)"));
			assert.ok(source.includes(`cp -R "$SRC/${name}/." "$DEST/${name}/"`));
			assert.ok(source.includes(`cp -R "$SRC/pi-ui/${name}/." "$DEST/ui/${name}/"`));
			assert.ok(source.includes(`import registerUI from "./ui/${name}/index.ts";`));
		}
	}
});

for (const name of ["worker", "pi-ui"]) for (const language of ["", "_EN"]) {
	test(`${name} ${language || "default"}: transcript request passes the actual capability schema`, () => {
		// Stop at the directory resolver: schema validation must pass first.
		// This checks the real validator without reading any user's recordings.
		const validated = new Error("schema accepted");
		const reader = new WorkerTranscriptReader(() => { throw validated; });
		const calls = examples(name, language)
			.filter(value => value.type === "prompt" && value.message?.startsWith("/pi-ui-rpc "))
			.map(value => JSON.parse(value.message.slice("/pi-ui-rpc ".length)))
			.filter(value => value.method === "worker.transcript");
		assert.ok(calls.length > 0, "guide must contain a complete transcript request");
		for (const call of calls) assert.throws(() => reader.read(call.params), error => error === validated);
	});
}

for (const language of ["", "_EN"]) {
	test(`questionnaire ${language || "ZH"}: documented submission passes the capability validator`, () => {
		const values = examples("ask-question", language);
		const request = values.find(value => value.title === "pi-goui:ask-question:v1");
		const response = values.find(value => value.type === "extension_ui_response" && value.id === request.id && value.value);
		const payload = JSON.parse(request.placeholder);
		assert.equal(validateQuestionResponse(JSON.parse(response.value), payload.questions).cancelled, false);
		const cancel = values.find(value => value.type === "extension_ui_response" && value.cancelled);
		assert.equal(cancel.id, request.id);
	});
	test(`Worker ${language || "ZH"}: documented settings retain authoritative schema`, () => {
		const values = examples("worker", language);
		const request = values.find(value => value.title === "pi-goui:worker-settings:v1");
		const payload = JSON.parse(request.placeholder);
		assert.deepEqual(parseSettings(payload.config), payload.config);
		const response = values.find(value => value.type === "extension_ui_response" && value.id === request.id && value.value);
		assert.ok(parseSettings(JSON.parse(response.value)));
	});
}
