#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"

echo "==> Building runAll..."
./build.sh

echo "==> Starting runAll..."
exec ./bin/runAll --config ../runAll.yaml "$@"
