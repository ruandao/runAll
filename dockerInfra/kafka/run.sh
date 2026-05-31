#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

COMPOSE_FILE="docker-compose.yml"

readonly -a REQUIRED_IMAGES=(
  "confluentinc/cp-zookeeper:7.4.0"
  "confluentinc/cp-kafka:7.4.0"
  "provectuslabs/kafka-ui:v0.7.2"
)

if ! command -v docker >/dev/null 2>&1; then
  echo "错误: 未找到 docker，请先安装 Docker Desktop 或 Docker Engine。" >&2
  exit 1
fi

compose() {
  if docker compose version >/dev/null 2>&1; then
    docker compose -f "$COMPOSE_FILE" "$@"
  elif docker-compose version >/dev/null 2>&1; then
    docker-compose -f "$COMPOSE_FILE" "$@"
  else
    echo "错误: 未找到 Docker Compose（需 docker compose 或 docker-compose）。" >&2
    exit 1
  fi
}

image_present() {
  docker image inspect "$1" >/dev/null 2>&1
}

ensure_images() {
  local missing=0
  for image in "${REQUIRED_IMAGES[@]}"; do
    if ! image_present "$image"; then
      missing=1
      echo "本地缺少镜像: $image"
    fi
  done
  if [[ "$missing" -eq 1 ]]; then
    echo "正在拉取缺失的 Kafka 栈镜像（仅一次）..."
    compose pull
  else
    echo "本地镜像已齐，跳过 pull。"
  fi
}

up_stack() {
  echo "正在启动 Kafka 栈（Zookeeper/Kafka/Kafka UI，不重复 pull）..."
  compose up -d --pull never --remove-orphans
}

down_stack() {
  echo "正在停止 Kafka 栈容器..."
  compose down --remove-orphans "$@"
}

mode="start"
if [[ $# -gt 0 ]]; then
  case "$1" in
    start|managed|stop)
      mode="$1"
      shift
      ;;
  esac
fi

case "$mode" in
  stop)
    down_stack "$@"
    ;;
  managed|start)
    ensure_images
    up_stack
    compose ps
    ;;
esac
