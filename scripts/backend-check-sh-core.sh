#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_NODE="${REMOTE_NODE:-sh-core}"
REMOTE_TMP_DIR="${REMOTE_TMP_DIR:-/tmp/rtime-status-board-backend-check-${USER:-rtime}-$$}"
GO_IMAGE="${GO_IMAGE:-golang:1.22-bookworm}"
GO_PROXY="${GO_PROXY:-https://goproxy.cn,direct}"
GO_TIMEOUT_SECONDS="${GO_TIMEOUT_SECONDS:-900}"
GO_STEP_TIMEOUT_SECONDS="${GO_STEP_TIMEOUT_SECONDS:-600}"
REMOTE_GO_CACHE_DIR="${REMOTE_GO_CACHE_DIR:-/tmp/rtime-status-board-go-cache}"
CLEAN_REMOTE_GO_CACHE="${CLEAN_REMOTE_GO_CACHE:-0}"
INVENTORY="${RTIME_INVENTORY:-$HOME/.ai-skills/rtime-remote/inventory-cache.json}"
RTIME_SSH="$HOME/.ai-skills/rtime-remote/scripts/rtime-ssh"

if [[ ! -x "$RTIME_SSH" ]]; then
  echo "[ERROR] rtime-ssh not found: $RTIME_SSH" >&2
  exit 1
fi

if [[ ! -f "$INVENTORY" ]]; then
  echo "[ERROR] inventory not found: $INVENTORY" >&2
  exit 1
fi

SSH_HOST="$(jq -r ".nodes[\"$REMOTE_NODE\"].ssh_host" "$INVENTORY")"
SSH_USER="$(jq -r ".nodes[\"$REMOTE_NODE\"].ssh_user" "$INVENTORY")"
SSH_KEY="$(jq -r ".nodes[\"$REMOTE_NODE\"].ssh_key" "$INVENTORY")"
SSH_KEY="${SSH_KEY/#\~/$HOME}"

cleanup() {
  "$RTIME_SSH" "$REMOTE_NODE" "rm -rf '$REMOTE_TMP_DIR'" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[INFO] Syncing backend check workspace to $REMOTE_NODE:$REMOTE_TMP_DIR"
"$RTIME_SSH" "$REMOTE_NODE" "rm -rf '$REMOTE_TMP_DIR' && mkdir -p '$REMOTE_TMP_DIR'"

rsync -az --delete \
  --exclude '.git/' \
  --exclude '.env' \
  --exclude '.env.production' \
  --exclude 'data/' \
  --exclude 'work/' \
  --exclude '__pycache__/' \
  --exclude '*.pyc' \
  --exclude 'frontend/node_modules/' \
  --exclude 'frontend/dist/' \
  --exclude 'coverage/' \
  -e "ssh -i $SSH_KEY -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null" \
  "$ROOT/" "$SSH_USER@$SSH_HOST:$REMOTE_TMP_DIR/"

remote_script="$(cat <<'REMOTE'
set -euo pipefail

cd "$REMOTE_TMP_DIR"
unset http_proxy HTTP_PROXY https_proxy HTTPS_PROXY all_proxy ALL_PROXY
export no_proxy="*"
export NO_PROXY="*"

run_id="$(basename "$REMOTE_TMP_DIR")"
config_container="rsb-backend-config-${run_id}"
test_container="rsb-backend-test-${run_id}"

cleanup_remote() {
  docker rm -f "$config_container" "$test_container" >/dev/null 2>&1 || true
  if [[ "${CLEAN_REMOTE_GO_CACHE:-0}" == "1" ]]; then
    rm -rf "$REMOTE_GO_CACHE_DIR"
  fi
}
trap cleanup_remote EXIT

mkdir -p "$REMOTE_GO_CACHE_DIR/pkgmod" "$REMOTE_GO_CACHE_DIR/build"

run_go_container() {
  container_name="$1"
  shift
  docker rm -f "$container_name" >/dev/null 2>&1 || true
  if command -v timeout >/dev/null 2>&1; then
    timeout --kill-after=10s "$GO_STEP_TIMEOUT_SECONDS" \
      docker run --name "$container_name" --rm \
        -v "$REMOTE_TMP_DIR":/workspace \
        -v "$REMOTE_GO_CACHE_DIR/pkgmod":/go/pkg/mod \
        -v "$REMOTE_GO_CACHE_DIR/build":/root/.cache/go-build \
        -e GOPROXY="$GO_PROXY" \
        -w /workspace/backend \
        "$GO_IMAGE" \
        "$@"
  else
    docker run --name "$container_name" --rm \
      -v "$REMOTE_TMP_DIR":/workspace \
      -v "$REMOTE_GO_CACHE_DIR/pkgmod":/go/pkg/mod \
      -v "$REMOTE_GO_CACHE_DIR/build":/root/.cache/go-build \
      -e GOPROXY="$GO_PROXY" \
      -w /workspace/backend \
      "$GO_IMAGE" \
      "$@"
  fi
}

echo "[REMOTE] backend config check"
run_go_container "$config_container" \
  go run ./cmd/statusd -config /workspace/config/status-board.yaml -check-config

echo "[REMOTE] backend tests"
run_go_container "$test_container" \
  go test ./...

echo "[REMOTE] backend check ok"
REMOTE
)"

remote_cmd="REMOTE_TMP_DIR=$(printf "%q" "$REMOTE_TMP_DIR")"
remote_cmd+=" GO_IMAGE=$(printf "%q" "$GO_IMAGE")"
remote_cmd+=" GO_PROXY=$(printf "%q" "$GO_PROXY")"
remote_cmd+=" GO_STEP_TIMEOUT_SECONDS=$(printf "%q" "$GO_STEP_TIMEOUT_SECONDS")"
remote_cmd+=" REMOTE_GO_CACHE_DIR=$(printf "%q" "$REMOTE_GO_CACHE_DIR")"
remote_cmd+=" CLEAN_REMOTE_GO_CACHE=$(printf "%q" "$CLEAN_REMOTE_GO_CACHE")"
remote_cmd+=" bash -lc $(printf "%q" "$remote_script")"

"$ROOT/scripts/run-with-timeout.sh" "$GO_TIMEOUT_SECONDS" "$RTIME_SSH" "$REMOTE_NODE" "$remote_cmd"

echo "[OK] sh-core backend check passed"
