# Engineering Guidelines

- Deliver the smallest complete solution that meets the agreed need. Defer optional features, not correctness or safety.
- Follow existing patterns unless correctness requires otherwise. Keep complexity proportional to real needs; avoid speculative abstractions and dependencies.
- Prefer deep modules: simple interfaces that hide meaningful complexity. Reduce what callers must know; avoid mere pass-through layers.
- Structure code by cohesive responsibility, with clear names and straightforward control flow. Extract helpers to clarify intent or remove meaningful duplication, not to add layers. Favor readability over cleverness or line-count reduction.
- Keep changes focused. Preserve unrelated behavior and existing user changes; avoid opportunistic refactoring.
- Add tests only when requested or justified by meaningful correctness or regression risk. Prefer a few focused behavioral tests; extend existing coverage and reuse setup instead of piling on redundant cases, mocks, or scaffolding.
- Verify in proportion to risk. Report checks and remaining uncertainty; stop when correctness is sufficiently supported by evidence.
