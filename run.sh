#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"

echo "==> Building runAll..."
go build -o runAll .

echo "==> Starting runAll..."
exec ./runAll --config ../config.yaml "$@"
