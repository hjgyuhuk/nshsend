#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/bin"
mkdir -p "$OUT_DIR"

EXT=""
if [[ "${GOOS:-$(go env GOOS)}" == "windows" ]]; then
  EXT=".exe"
fi

echo "Building nshsend-core..."
(
  cd "$ROOT_DIR/core"
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$OUT_DIR/nshsend-core$EXT" ./cmd/nshsend-core
)
echo "Built $OUT_DIR/nshsend-core$EXT"
