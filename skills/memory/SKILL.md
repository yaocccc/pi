---
name: memory
description: For complex tasks, proactively search indexed memory with memory_search before acting when prior project decisions, constraints, fixes, or conventions could affect the result—even if the user does not mention earlier work. Read a matched memory by name with memory_get only when needed, and request memory_summarize after completed work produces durable reusable knowledge. Skip search for routine self-contained edits, translations, summaries, and general informational questions.
---

# Memory

Use indexed memory when the current task depends on prior reusable context, or when completed work has produced durable knowledge worth preserving.

## Rules

1. Before acting on a complex task, proactively call `memory_search` once when prior project decisions, constraints, fixes, conventions, or attempted approaches have a reasonable chance of affecting the result. Do this even if the user does not mention prior work; if uncertain whether relevant project history exists, prefer searching.
2. Make the initial search bounded and focused. Use the project name when known plus distinctive feature, module, symbol, error, technology, or intended-behavior terms. If no useful match is found, continue with the current repository and user request; do not repeatedly search with vague variations unless the task later reveals a specific missing fact.
3. `memory_search` only searches `~/.pi/agent/memory-index.md` and returns matching index entries.
4. Do not search `~/.pi/agent/memory.md`; it is user profile memory, not indexed memory.
5. Do not scan `~/.pi/agent/memories/` directly.
6. Review the index results first. If their summaries are sufficient, continue without loading details.
7. When details are needed, call `memory_get` with the exact memory name returned by `memory_search`, omitting its leading four-digit index number.
8. Read only the specific matched memory needed for the current task.
9. Treat a successful `memory_get` result as session context: do not call it again for the same memory unless that memory may have changed. When called again, the tool compares the latest version with the most recent read in the current branch, returns full details only if changed, and otherwise instructs you to reuse the existing Tool Result.
10. Prefer current project matches, then `global` matches.
11. Search results are candidate context only; if they conflict with current repository files, current files win.
12. Call `memory_summarize` exactly once at the end of the current task only when the completed result has durable reuse value.
13. `memory_summarize` only queues a temporary request; it summarizes once after the current agent run fully settles, so finish the user-facing response normally.
14. Never call `memory_summarize` more than once in the same user turn.

## When To Search

Before acting, run one focused search for complex tasks such as:

- implementing or changing substantial behavior in an existing project
- debugging recurring, ambiguous, cross-module, or previously attempted problems
- architecture, refactoring, migration, performance, security, or compatibility work
- changes spanning multiple files, services, APIs, schemas, dependencies, or workflows
- decisions that may be constrained by existing project behavior, conventions, or prior trade-offs
- tasks containing “之前”, “上次”, “继续”, “按之前”, “和之前一样”, or “还记得”
- any task where missing prior context could reasonably cause rework or an incompatible solution

Complexity—not explicit wording—is the main trigger. Do not wait for the user to mention memory or prior work.

Do not search for routine self-contained edits, translations, summaries of provided content, simple explanations, general informational questions, isolated code generation with no project history, or trivial mechanical changes whose result cannot reasonably depend on prior decisions.

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
