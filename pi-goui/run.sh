#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$ROOT/bin"
go -C "$ROOT" build -trimpath -o "$ROOT/bin/" ./cmd/pi-goui ./cmd/webui
exec "$ROOT/bin/pi-goui" "$@"
