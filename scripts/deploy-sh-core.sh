#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_NODE="${REMOTE_NODE:-sh-core}"
REMOTE_DIR="${REMOTE_DIR:-/opt/rtime-status-board}"
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

echo "[INFO] Syncing $ROOT to $REMOTE_NODE:$REMOTE_DIR"
"$RTIME_SSH" "$REMOTE_NODE" "mkdir -p '$REMOTE_DIR'"

rsync -az --delete \
  --exclude '.git/' \
  --exclude '.env' \
  --exclude '.env.production' \
  --exclude 'data/' \
  --exclude 'work/' \
  --exclude '__pycache__/' \
  --exclude '*.pyc' \
  --exclude 'frontend/node_modules/' \
  --exclude 'coverage/' \
  -e "ssh -i $SSH_KEY -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null" \
  "$ROOT/" "$SSH_USER@$SSH_HOST:$REMOTE_DIR/"

"$RTIME_SSH" "$REMOTE_NODE" "cd '$REMOTE_DIR' && test -f .env.production || cp .env.example .env.production"

if ! "$RTIME_SSH" "$REMOTE_NODE" "docker compose version >/dev/null 2>&1"; then
  echo "[ERROR] docker compose plugin is missing on $REMOTE_NODE."
  echo "        Run: make install-compose-sh-core"
  exit 2
fi

"$RTIME_SSH" "$REMOTE_NODE" "cd '$REMOTE_DIR' && docker compose -p rtime-status-board -f compose.prod.yml --env-file .env.production config >/tmp/rtime-status-board.prod.compose.yml"
echo "[OK] Deployment files are on $REMOTE_NODE:$REMOTE_DIR"
