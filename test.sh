#!/bin/bash
# 单元测试 + 编译冒烟（含 build_test.go 集成校验）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

echo "==> go test ./..."
go test ./... -race -cover "$@"

echo "==> All tests passed."
