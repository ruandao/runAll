#!/bin/bash
# 编译 runAll：源码在 src/，产物在 bin/runAll
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

mkdir -p bin

echo "==> Building runAll (./src -> bin/runAll)..."
go build -o bin/runAll ./src

echo "==> Done: $ROOT/bin/runAll"
