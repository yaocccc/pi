# Optional Worker adapter

[中文](README.md)

This package adapts the third-party Worker extension's structured recordings for the Go inspector. It is not part of generic Pi session persistence and does not import or locate the TypeScript extension.

- `Transcript` contains only a version and recording UUID.
- `Cursor.Read(ctx, client, descriptor)` calls `worker.transcript` through `rpc.Client.CallExtension` and receives complete records, an opaque continuation token, and reset/more/partial flags.
- The backend owns path validation, storage layout, legacy lookup, file identity checks, incremental offsets, and limits. No production file in this package reads the filesystem.
- `Replay` and `EventBatch` reconstruct display state only. They cannot resume or execute a Worker.
- Without the optional RPC bridge or capability, the host shows the task's existing result instead. There is no local-file fallback.

The Pi-side method is registered by `extensions/pi-ui/worker/index.ts` and delegates to `worker:v1.readTranscript`. The capability reads at most 4 MiB per request, validates the recording header/UUID, accepts only regular non-symlink files, and maintains bounded, expiring backend cursors. Removed, replaced or truncated files reset or fail safely; a cursor that reached primary storage never falls back to an obsolete legacy copy.

Tests: `cd pi-goui && go test ./internal/extensions/worker`. Go tests do not depend on this repository's TypeScript paths.
