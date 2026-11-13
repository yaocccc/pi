// Maintainer-only: checked-in support makes each capability/UI pair copyable.
// Consumers copy directories as-is; no generation, install hook or shared/ needed.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = new URL("../", import.meta.url);
const modules = ["ask-question", "worker", "codex-fast", "autoname", "filter-output", "sol-pi"];
const copies = [];
for (const name of modules) {
  copies.push(["shared/capabilities.ts", `${name}/capabilities.ts`]);
  copies.push(["pi-ui/shared/portable.ts", `pi-ui/${name}/support.ts`, [["../../shared/capabilities.ts", `../../${name}/capabilities.ts`]]]);
}
for (const name of ["worker", "codex-fast"]) copies.push(["pi-ui/rpc.ts", `pi-ui/${name}/bridge.ts`, [["../shared/capabilities.ts", `../../${name}/capabilities.ts`]]]);
for (const name of ["autoname", "telegram"]) copies.push(["shared/session-policy.ts", `${name}/session-policy.ts`]);
copies.push(["shared/token-estimate.ts", "worker/token-estimate.ts"]);
copies.push(["pi-ui/shared/frame.ts", "pi-ui/ask-question/frame.ts"]);
copies.push(["pi-ui/shared/tool-tokens.ts", "pi-ui/worker/tool-tokens.ts", [["../../shared/token-estimate.ts", "../../worker/token-estimate.ts"]]]);

let stale = false;
for (const [source, target, replacements = []] of copies) {
  let content = await readFile(new URL(source, root), "utf8");
  for (const [from, to] of replacements) {
    if (!content.includes(from)) throw new Error(`Missing import to rewrite in ${source}: ${from}`);
    content = content.replaceAll(from, to);
  }
  content = `// Bundled support from ${source}; update via npm run sync:portable.\n// Self-contained at runtime: consumers do not need the source template.\n${content}`;
  const destination = new URL(target, root);
  if (process.argv.includes("--check")) {
    const existing = await readFile(destination, "utf8").catch(() => "");
    if (existing !== content) { console.error(`Stale bundled support: ${target}`); stale = true; }
  } else {
    await mkdir(dirname(fileURLToPath(destination)), { recursive: true });
    await writeFile(destination, content);
  }
}
if (stale) process.exitCode = 1;
else console.log(`${process.argv.includes("--check") ? "Checked" : "Updated"} ${copies.length} bundled support files`);
