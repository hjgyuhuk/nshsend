#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"$ROOT_DIR/scripts/build-core.sh"

export PATH="$ROOT_DIR/bin:$PATH"
cd "$ROOT_DIR"
exec nsh run
