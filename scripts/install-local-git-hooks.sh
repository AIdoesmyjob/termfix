#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
git -C "$ROOT_DIR" config core.hooksPath .githooks
echo "Installed repository-local hooks from $ROOT_DIR/.githooks"
