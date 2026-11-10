#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$ROOT/bin"
go -C "$ROOT" build -trimpath -o "$ROOT/bin/pi-goui" ./cmd/pi-goui
exec "$ROOT/bin/pi-goui" "$@"
