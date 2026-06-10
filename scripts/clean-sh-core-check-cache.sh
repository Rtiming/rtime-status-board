#!/usr/bin/env bash
set -euo pipefail

REMOTE_NODE="${REMOTE_NODE:-sh-core}"
REMOTE_GO_CACHE_DIR="${REMOTE_GO_CACHE_DIR:-/tmp/rtime-status-board-go-cache}"
RTIME_SSH="$HOME/.ai-skills/rtime-remote/scripts/rtime-ssh"

if [[ ! -x "$RTIME_SSH" ]]; then
  echo "[ERROR] rtime-ssh not found: $RTIME_SSH" >&2
  exit 1
fi

remote_script="$(cat <<'REMOTE'
set -euo pipefail

containers="$(docker ps -aq --filter name='^/rsb-backend-' 2>/dev/null || true)"
if [[ -n "$containers" ]]; then
  docker rm -f $containers >/dev/null
fi

rm -rf /tmp/rtime-status-board-backend-check-* "$REMOTE_GO_CACHE_DIR"

echo "[REMOTE] backend check cache cleaned"
REMOTE
)"

remote_cmd="REMOTE_GO_CACHE_DIR=$(printf "%q" "$REMOTE_GO_CACHE_DIR")"
remote_cmd+=" bash -lc $(printf "%q" "$remote_script")"

"$RTIME_SSH" "$REMOTE_NODE" "$remote_cmd"
echo "[OK] sh-core backend check cache cleaned"
