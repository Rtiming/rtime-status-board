#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-up}"
REMOTE_NODE="${REMOTE_NODE:-sh-core}"
REMOTE_DIR="${REMOTE_DIR:-/opt/rtime-status-board}"
PROJECT_NAME="${PROJECT_NAME:-rtime-status-board}"
GO_IMAGE="${GO_IMAGE:-golang:1.22-bookworm}"
GO_PROXY="${GO_PROXY:-https://goproxy.cn,direct}"
GO_STEP_TIMEOUT_SECONDS="${GO_STEP_TIMEOUT_SECONDS:-600}"
REMOTE_GO_CACHE_DIR="${REMOTE_GO_CACHE_DIR:-/tmp/rtime-status-board-go-cache}"
RTIME_SSH="$HOME/.ai-skills/rtime-remote/scripts/rtime-ssh"

if [[ ! -x "$RTIME_SSH" ]]; then
  echo "[ERROR] rtime-ssh not found: $RTIME_SSH" >&2
  exit 1
fi

case "$ACTION" in
  up|logs|ps|down) ;;
  *)
    echo "[ERROR] unsupported action: $ACTION" >&2
    echo "        expected: up, logs, ps, down" >&2
    exit 2
    ;;
esac

remote_cmd="cd $(printf "%q" "$REMOTE_DIR")"
remote_cmd+=" && PROJECT_NAME=$(printf "%q" "$PROJECT_NAME")"
remote_cmd+=" GO_IMAGE=$(printf "%q" "$GO_IMAGE")"
remote_cmd+=" GO_PROXY=$(printf "%q" "$GO_PROXY")"
remote_cmd+=" GO_STEP_TIMEOUT_SECONDS=$(printf "%q" "$GO_STEP_TIMEOUT_SECONDS")"
remote_cmd+=" REMOTE_GO_CACHE_DIR=$(printf "%q" "$REMOTE_GO_CACHE_DIR")"
remote_cmd+=" bash scripts/prod-compose-remote.sh $(printf "%q" "$ACTION")"

"$RTIME_SSH" "$REMOTE_NODE" "$remote_cmd"
