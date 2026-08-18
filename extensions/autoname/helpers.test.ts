import assert from "node:assert/strict";
import test from "node:test";
import {
    extractNamingMessages,
    hasConversationPair,
    isExtensionOwnedName,
    latestAutonameProvenance,
    parseNamingDecision,
    validateName,
} from "./helpers.ts";

test("extractNamingMessages includes only user and assistant text blocks", () => {
    const messages = extractNamingMessages([
        { type: "message", message: { role: "user", content: "Implement session naming." } },
        { type: "message", message: { role: "assistant", content: [
            { type: "thinking", thinking: "private plan" },
            { type: "text", text: "I will add an extension." },
            { type: "toolCall", name: "write" },
        ] } },
        { type: "message", message: { role: "toolResult", content: [{ type: "text", text: "secret output" }] } },
        { type: "message", message: { role: "custom", content: "extension prompt" } },
        { type: "custom", customType: "other", data: { hidden: true } },
    ]);

    assert.deepEqual(messages.map((message) => [message.role, message.content[0].text]), [
        ["user", "Implement session naming."],
        ["assistant", "I will add an extension."],
    ]);
    assert.equal(hasConversationPair(messages), true);
});

test("parseNamingDecision accepts only strict decision objects", () => {
    assert.deepEqual(parseNamingDecision('{"action":"keep"}'), { action: "keep" });
    assert.deepEqual(parseNamingDecision('```json\n{"action":"rename","name":"Autoname extension"}\n```'), {
        action: "rename",
        name: "Autoname extension",
    });
    assert.equal(parseNamingDecision('{"action":"rename","name":"x","extra":true}'), undefined);
    assert.equal(parseNamingDecision('rename it'), undefined);
    assert.equal(validateName("  Build   autoname extension  "), "Build autoname extension");
    assert.equal(validateName("\nBad\tname"), "Bad name");
    assert.equal(validateName("x"), undefined);
});

test("provenance is read from the active branch and protects manual names", () => {
    const branch = [
        { type: "custom", customType: "autoname", data: { version: 1, kind: "set-name", name: "Old title" } },
        { type: "custom", customType: "autoname", data: { version: 1, kind: "set-name", name: "Current title" } },
    ];
    assert.deepEqual(latestAutonameProvenance(branch), { version: 1, kind: "set-name", name: "Current title" });
    assert.equal(isExtensionOwnedName("Current title", branch), true);
    assert.equal(isExtensionOwnedName("Manual title", branch), false);
});
