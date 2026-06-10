#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_NODE="${REMOTE_NODE:-sh-core}"
REMOTE_DIR="${REMOTE_DIR:-/opt/rtime-status-board}"
STATUS_BOARD_ENV_FILE="${STATUS_BOARD_ENV_FILE:-.env.production}"
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

cleanup_script="$(cat <<'REMOTE'
set -euo pipefail
cd "$REMOTE_DIR"
rm -rf .git .env data work coverage tmp node_modules frontend/node_modules
find . -type d -name __pycache__ -prune -exec rm -rf {} +
find . -type f \( -name '*.pyc' -o -name '.DS_Store' \) -delete
REMOTE
)"
"$RTIME_SSH" "$REMOTE_NODE" "REMOTE_DIR=$(printf "%q" "$REMOTE_DIR") bash -lc $(printf "%q" "$cleanup_script")"

"$RTIME_SSH" "$REMOTE_NODE" "cd '$REMOTE_DIR' && test -f .env.production || cp .env.example .env.production"

read_env_value() {
  local key="$1"
  if [[ -f "$STATUS_BOARD_ENV_FILE" ]]; then
    awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); gsub(/^"|"$/, ""); print; exit }' "$STATUS_BOARD_ENV_FILE"
  fi
}

PUBLIC_DOMAIN="$(read_env_value STATUS_BOARD_PUBLIC_DOMAIN)"
PUBLIC_IP="$(read_env_value STATUS_BOARD_PUBLIC_IP)"
TAILNET_STATUS_URL="$(read_env_value STATUS_BOARD_TAILNET_URL)"

public_env_script="$(cat <<'REMOTE'
set -euo pipefail
cd "$REMOTE_DIR"
env_file=".env.production"
backup="$env_file.bak.public.$(date +%Y%m%d-%H%M%S)"
tmp="$(mktemp)"
cp -p "$env_file" "$backup"
awk -F= '
  $1 == "STATUS_BOARD_PUBLIC_DOMAIN" { next }
  $1 == "STATUS_BOARD_PUBLIC_IP" { next }
  $1 == "STATUS_BOARD_TAILNET_URL" { next }
  { print }
' "$env_file" >"$tmp"
changed=()
append_if_real() {
  local key="$1"
  local value="$2"
  local placeholder="$3"
  if [[ -n "$value" && "$value" != "$placeholder" ]]; then
    printf "%s=%s\n" "$key" "$value" >>"$tmp"
    changed+=("$key")
  fi
}
append_if_real STATUS_BOARD_PUBLIC_DOMAIN "$STATUS_BOARD_PUBLIC_DOMAIN" "status.example.com"
append_if_real STATUS_BOARD_PUBLIC_IP "$STATUS_BOARD_PUBLIC_IP" "203.0.113.10"
append_if_real STATUS_BOARD_TAILNET_URL "$STATUS_BOARD_TAILNET_URL" "http://100.64.10.5:18083"
if [[ "${#changed[@]}" -gt 0 ]]; then
  cat "$tmp" >"$env_file"
  chmod 600 "$env_file"
  printf "[OK] Synced remote public env metadata keys:"
  printf " %s" "${changed[@]}"
  printf "\n"
  printf "[INFO] Remote env backup: %s/%s\n" "$REMOTE_DIR" "$backup"
else
  rm -f "$backup"
  printf "[INFO] No real public env metadata keys found in local %s; remote env left without public metadata sync\n" "$STATUS_BOARD_ENV_FILE"
fi
rm -f "$tmp"
REMOTE
)"
"$RTIME_SSH" "$REMOTE_NODE" \
  "REMOTE_DIR=$(printf "%q" "$REMOTE_DIR") STATUS_BOARD_ENV_FILE=$(printf "%q" "$STATUS_BOARD_ENV_FILE") STATUS_BOARD_PUBLIC_DOMAIN=$(printf "%q" "$PUBLIC_DOMAIN") STATUS_BOARD_PUBLIC_IP=$(printf "%q" "$PUBLIC_IP") STATUS_BOARD_TAILNET_URL=$(printf "%q" "$TAILNET_STATUS_URL") bash -lc $(printf "%q" "$public_env_script")"

if ! "$RTIME_SSH" "$REMOTE_NODE" "docker compose version >/dev/null 2>&1"; then
  echo "[ERROR] docker compose plugin is missing on $REMOTE_NODE."
  echo "        Run: make install-compose-sh-core"
  exit 2
fi

"$RTIME_SSH" "$REMOTE_NODE" "cd '$REMOTE_DIR' && docker compose -p rtime-status-board -f compose.prod.yml --env-file .env.production config >/tmp/rtime-status-board.prod.compose.yml"
echo "[OK] Deployment files are on $REMOTE_NODE:$REMOTE_DIR"
