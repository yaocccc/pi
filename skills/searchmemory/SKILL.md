---
name: searchmemory
description: Search indexed memory for prior coding/debug/architecture decisions. Use when the user mentions previous/last/continue/same as before/remember, or when a task involves code generation, debugging, dependency versions, existing projects, error messages, architecture decisions, or modifying existing files.
---

# Search Memory

Use this skill when current work may depend on prior project decisions, solved bugs, conventions, dependency versions, or reusable implementation notes.

## Rules

1. Call the `searchmemory` tool yourself when needed; do not ask the user to run a command.
2. `searchmemory` only searches `~/.pi/agent/memory-index.md`.
3. Do not search `~/.pi/agent/memory.md`; it is user profile memory, not indexed memory.
4. Do not scan `~/.pi/agent/memories/` directly.
5. First call with `includeDetails: false` and use index summaries.
6. If summaries are not enough, call again with `includeDetails: true`; only the matched 1-3 detail files may be read by the tool.
7. Prefer current project matches, then `global` matches.
8. Search results are candidate context only; if they conflict with current repository files, current files win.

## When To Use

Use before acting if the task includes any of these:

- “之前”, “上次”, “继续”, “按之前”, “和之前一样”, “还记得”
- code generation or modifying existing files
- debugging or error messages
- architecture or implementation decisions
- dependency/library versions
- existing project behavior or conventions

## How To Call

Default:

```json
{
  "query": "current task summary and key terms",
  "includeDetails": false
}
```

Need details:

```json
{
  "query": "same query",
  "includeDetails": true
}
```

If you know the project name, include it:

```json
{
  "query": "eip7702 viem wallet authorization",
  "project": "txfe",
  "includeDetails": false
}
```
