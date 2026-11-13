#!/usr/bin/env bash
# Checks the Go code: go vet, then every test (the ones of engine/worker and
# the ones you write in sandbox/, in files named *_test.go).
#
# The tests run the way the server runs: compiled to WebAssembly, here in
# Node.js. Arguments go to `go test`: engine/test.sh -v -run TestMatchRoute
set -euo pipefail

ENGINE_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$ENGINE_DIR/env.sh"

# The program that runs a WebAssembly test binary in Node.js comes with Go.
GOROOT_DIR="$(go env GOROOT)"
RUNNER="$GOROOT_DIR/lib/wasm/go_js_wasm_exec"          # Go >= 1.24
[ -f "$RUNNER" ] || RUNNER="$GOROOT_DIR/misc/wasm/go_js_wasm_exec"  # Go < 1.24

# Only the folders of the project: "./..." would also enter engine/tools/go,
# the Go that scripts/install.py downloads.
PACKAGES=(./deps/... ./sandbox/... ./engine/worker/...)

cd "$ENGINE_DIR/.."
export GOOS=js GOARCH=wasm
go vet "${PACKAGES[@]}"
go test -exec "$RUNNER" "$@" "${PACKAGES[@]}"
