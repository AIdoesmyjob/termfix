#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

go mod download
go build ./...
go test ./... -race -count=1
go vet ./...

if command -v golangci-lint >/dev/null 2>&1; then
  golangci-lint run
else
  echo "[local-check] golangci-lint is not installed; build, race tests, and vet passed."
fi
