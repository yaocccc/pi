import assert from "node:assert/strict";
import test from "node:test";
import {
	buildImpeccablePrompt,
	findImpeccableCommand,
	IMPECCABLE_COMMANDS,
} from "./commands.ts";

const primaryCommands = [
	"shape", "init", "document", "extract", "critique", "audit", "polish", "bolder", "quieter",
	"distill", "harden", "onboard", "animate", "colorize", "typeset", "layout", "delight", "overdrive",
	"clarify", "adapt", "optimize", "live",
];
const hookActions = ["on", "off", "status", "ignore-rule", "ignore-file", "ignore-value", "reset"];

test("目录覆盖可用的 Impeccable Commands、别名和管理指令", () => {
	const commands = new Set(IMPECCABLE_COMMANDS.map((item) => item.command));
	for (const command of primaryCommands) assert.ok(commands.has(command), `missing ${command}`);
	for (const action of hookActions) assert.ok(commands.has(`hooks ${action}`), `missing hooks ${action}`);
	for (const command of ["doctor", "pin", "unpin"]) assert.ok(commands.has(command), `missing ${command}`);
	assert.equal(findImpeccableCommand("craft"), undefined);
	assert.equal(findImpeccableCommand("teach"), undefined);
	assert.equal(findImpeccableCommand("  HoOkS   StAtUs ")?.command, "hooks status");
});

test("需要目标的指令生成中文占位模板", () => {
	const shape = findImpeccableCommand("shape");
	assert.ok(shape);
	assert.equal(shape.invocation, "shape [目标]");
	assert.equal(buildImpeccablePrompt(shape), "/skill:impeccable shape [请填写：目标]");
});

test("不需要目标的指令只生成命令并保留末尾空格", () => {
	const live = findImpeccableCommand("live");
	assert.ok(live);
	assert.equal(buildImpeccablePrompt(live), "/skill:impeccable live ");
});

test("未知指令不会匹配", () => {
	assert.equal(findImpeccableCommand("not-a-command"), undefined);
});
