#!/usr/bin/env bash
# Compiles the Go program (engine/worker, which runs /sandbox) to WebAssembly
# in engine/build/.
# Called automatically by Wrangler (see "build" in wrangler.jsonc, at the root).
set -euo pipefail

ENGINE_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$ENGINE_DIR/env.sh"

mkdir -p "$ENGINE_DIR/build"

# Go's JS runtime must be the same version as the compiler.
GOROOT_DIR="$(go env GOROOT)"
WASM_EXEC="$GOROOT_DIR/lib/wasm/wasm_exec.js"          # Go >= 1.24
[ -f "$WASM_EXEC" ] || WASM_EXEC="$GOROOT_DIR/misc/wasm/wasm_exec.js"  # Go < 1.24
cp "$WASM_EXEC" "$ENGINE_DIR/build/wasm_exec.js"

cd "$ENGINE_DIR/.."
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o "$ENGINE_DIR/build/server.wasm" ./engine/worker
