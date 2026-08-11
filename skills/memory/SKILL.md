---
name: memory
description: Autonomously search indexed memory with memory_search when a task likely depends on prior work, read a matched memory by name with memory_get only when needed, and request memory_summarize after completed work produces durable reusable knowledge. Do not use for routine self-contained edits or summaries.
---

# Memory

Use indexed memory when the current task depends on prior reusable context, or when completed work has produced durable knowledge worth preserving.

## Rules

1. Decide autonomously when prior memory is likely to affect the result, then call `memory_search`; do not wait for the user to request it and do not ask the user to run it.
2. `memory_search` only searches `~/.pi/agent/memory-index.md` and returns matching index entries.
3. Do not search `~/.pi/agent/memory.md`; it is user profile memory, not indexed memory.
4. Do not scan `~/.pi/agent/memories/` directly.
5. Review the index results first. If their summaries are sufficient, continue without loading details.
6. When details are needed, call `memory_get` with the exact memory name returned by `memory_search`, omitting its leading four-digit index number.
7. Read only the specific matched memory needed for the current task.
8. Treat a successful `memory_get` result as session context: do not call it again for the same memory unless that memory may have changed. When called again, the tool compares the latest version with the most recent read in the current branch, returns full details only if changed, and otherwise instructs you to reuse the existing Tool Result.
9. Prefer current project matches, then `global` matches.
10. Search results are candidate context only; if they conflict with current repository files, current files win.
11. Call `memory_summarize` exactly once at the end of the current task only when the completed result has durable reuse value.
12. `memory_summarize` only queues a temporary request; it summarizes once after the current agent run fully settles, so finish the user-facing response normally.
13. Never call `memory_summarize` more than once in the same user turn.

## When To Search

Use before acting if the task includes any of these:

- “之前”, “上次”, “继续”, “按之前”, “和之前一样”, “还记得”
- recurring debugging errors or a previously attempted fix
- architecture or implementation decisions that may already have project history
- dependency/library versions tied to an existing project
- existing project behavior, conventions, constraints, or established workflows

Do not search for routine self-contained edits, isolated code generation, first-time implementations with no prior context, or general informational questions.

## When To Summarize

Call `memory_summarize` only after completing work that produced at least one of these:

- a resolved and verified bug with a reusable root cause or fix
- an explicit architecture or implementation decision
- a new project convention, constraint, or stable workflow
- a verified workaround or pitfall likely to recur
- a meaningful update that should be merged into an existing memory

Do not call it for routine formatting, minor one-off edits, incomplete investigations, unverified guesses, general explanations, or work still awaiting user decisions.

## How To Call

Search the index first:

```json
{
  "query": "current task summary and key terms"
}
```

If you know the project name:

```json
{
  "query": "eip7702 viem wallet authorization",
  "project": "txfe"
}
```

When a matched index entry requires full details, pass its memory name without the leading index number:

```json
{
  "name": "BackRun 整体配置、签名认证与 D1 存储"
}
```

After the task is complete and meets the summarize criteria, queue one summary request:

```json
{}
```
