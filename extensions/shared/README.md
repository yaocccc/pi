# Shared extension helpers

Pure frame colors and tool-token helpers used by Worker and ask-question. This directory intentionally has no `index.ts` or extension manifest, so Pi does not discover it as an extension.

The former `extensions/ui/` is archived unchanged at the repository root `ui/`. It is not automatically loaded. Its original relative imports are preserved; restore it to `extensions/ui/` before using it as a native UI extension again. Active extensions must not import the archive.

Restart Pi / pi-goui to start fresh backend processes without the old UI extension; moving files does not remove patches from already running processes.
