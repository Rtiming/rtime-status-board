#!/usr/bin/env bash
set -euo pipefail

REMOTE_NODE="${REMOTE_NODE:-sh-core}"
RTIME_SSH="$HOME/.ai-skills/rtime-remote/scripts/rtime-ssh"

if [[ ! -x "$RTIME_SSH" ]]; then
  echo "[ERROR] rtime-ssh not found: $RTIME_SSH" >&2
  exit 1
fi

"$RTIME_SSH" "$REMOTE_NODE" "apt-get update && (apt-get install -y docker-compose-plugin || apt-get install -y docker-compose-v2)"
"$RTIME_SSH" "$REMOTE_NODE" "docker compose version"
