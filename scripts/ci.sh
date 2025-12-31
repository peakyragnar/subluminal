#!/usr/bin/env bash
#
# scripts/ci.sh - Local CI that mirrors GitHub Actions exactly
#
# This is the ONLY truth oracle for "does CI pass?"
# Must produce identical pass/fail as .github/workflows/test.yml
#
set -euo pipefail

echo "=== CI: Go version ==="
go version

echo "=== CI: Building binaries ==="
go build -o bin/shim ./cmd/shim
go build -o bin/fakemcp ./cmd/fakemcp

echo "=== CI: Running tests ==="
export SUBLUMINAL_SHIM_PATH="$(pwd)/bin/shim"
go test -v ./...

echo "=== CI: All checks passed ==="
