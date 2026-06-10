#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RTIME_SSH="$HOME/.ai-skills/rtime-remote/scripts/rtime-ssh"
RTIME_PUSH="$HOME/.ai-skills/rtime-remote/scripts/rtime-push"
TOKEN="${STATUS_BOARD_AGENT_TOKEN:-}"
TARGETS="${TARGETS:-sh-core overseas orangepi rpi4 srv03}"
STATUS_BOARD_ENV_FILE="${STATUS_BOARD_ENV_FILE:-$ROOT/.env.production}"

read_env_value() {
  local key="$1"
  if [[ -f "$STATUS_BOARD_ENV_FILE" ]]; then
    awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); gsub(/^"|"$/, ""); print; exit }' "$STATUS_BOARD_ENV_FILE"
  fi
}

if [[ -z "$TOKEN" ]]; then
  TOKEN="$(read_env_value STATUS_BOARD_AGENT_TOKEN)"
fi

STATUS_BOARD_URL="${STATUS_BOARD_URL:-$(read_env_value STATUS_BOARD_URL)}"
if [[ -z "$STATUS_BOARD_URL" ]]; then
  tailnet_url="${STATUS_BOARD_TAILNET_URL:-$(read_env_value STATUS_BOARD_TAILNET_URL)}"
  tailnet_url="${tailnet_url:-http://100.64.10.5:18083}"
  STATUS_BOARD_URL="${tailnet_url%/}/api/v1/metrics/report/v2"
fi

if [[ -z "$TOKEN" || "$TOKEN" == "change-me" ]]; then
  echo "[ERROR] Set STATUS_BOARD_AGENT_TOKEN in the environment or $STATUS_BOARD_ENV_FILE before installing agents." >&2
  exit 1
fi

for node in $TARGETS; do
  echo "[INFO] Installing metrics agent on $node"
  "$RTIME_SSH" "$node" "rm -rf /tmp/rtime-status-agent-install && mkdir -p /tmp/rtime-status-agent-install"
  "$RTIME_PUSH" "$ROOT/deploy/agent/rtime-status-agent.py" "$node:/tmp/rtime-status-agent-install/rtime-status-agent.py"
  "$RTIME_PUSH" "$ROOT/deploy/agent/rtime-status-agent.service" "$node:/tmp/rtime-status-agent-install/rtime-status-agent.service"
  "$RTIME_PUSH" "$ROOT/deploy/agent/rtime-status-agent.timer" "$node:/tmp/rtime-status-agent-install/rtime-status-agent.timer"
  "$RTIME_SSH" "$node" "if [ \"\$(id -u)\" -eq 0 ]; then
  INSTALL_MODE='system'
  SUDO=''
elif sudo -n true 2>/dev/null; then
  INSTALL_MODE='system'
  SUDO='sudo -n'
else
  INSTALL_MODE='user'
  SUDO=''
fi

if [ \"\$INSTALL_MODE\" = 'system' ]; then
  \$SUDO install -d -m 755 /opt/rtime-status-agent
  \$SUDO install -m 755 /tmp/rtime-status-agent-install/rtime-status-agent.py /opt/rtime-status-agent/rtime-status-agent.py
  \$SUDO install -m 644 /tmp/rtime-status-agent-install/rtime-status-agent.service /etc/systemd/system/rtime-status-agent.service
  \$SUDO install -m 644 /tmp/rtime-status-agent-install/rtime-status-agent.timer /etc/systemd/system/rtime-status-agent.timer
  \$SUDO tee /etc/rtime-status-agent.env >/dev/null <<EOF
STATUS_BOARD_NODE_ID=$node
STATUS_BOARD_URL=$STATUS_BOARD_URL
STATUS_BOARD_REPORT_VERSION=2
STATUS_BOARD_AGENT_TOKEN=$TOKEN
EOF
  \$SUDO chmod 600 /etc/rtime-status-agent.env
  \$SUDO systemctl daemon-reload
  \$SUDO systemctl enable --now rtime-status-agent.timer
  \$SUDO systemctl start rtime-status-agent.service
  echo '[OK] Installed systemd metrics agent'
else
  USER_AGENT_DIR=\"\$HOME/.local/share/rtime-status-agent\"
  USER_ENV_DIR=\"\$HOME/.config/rtime-status-agent\"
  install -d -m 755 \"\$USER_AGENT_DIR\"
  install -d -m 700 \"\$USER_ENV_DIR\"
  install -m 755 /tmp/rtime-status-agent-install/rtime-status-agent.py \"\$USER_AGENT_DIR/rtime-status-agent.py\"
  cat > \"\$USER_ENV_DIR/env\" <<EOF
STATUS_BOARD_NODE_ID=$node
STATUS_BOARD_URL=$STATUS_BOARD_URL
STATUS_BOARD_REPORT_VERSION=2
STATUS_BOARD_AGENT_TOKEN=$TOKEN
EOF
  chmod 600 \"\$USER_ENV_DIR/env\"
  if ! command -v crontab >/dev/null 2>&1; then
    echo '[ERROR] Need sudo/root or user crontab to install metrics agent.' >&2
    exit 1
  fi
  CRON_CMD=\"set -a; . \$USER_ENV_DIR/env; set +a; /usr/bin/python3 \$USER_AGENT_DIR/rtime-status-agent.py >/tmp/rtime-status-agent.log 2>&1\"
  TMP_CRON=\"\$(mktemp)\"
  crontab -l 2>/dev/null | grep -v 'rtime-status-agent.py' > \"\$TMP_CRON\" || true
  printf '%s\n' \"* * * * * \$CRON_CMD\" >> \"\$TMP_CRON\"
  crontab \"\$TMP_CRON\"
  rm -f \"\$TMP_CRON\"
  sh -c \"\$CRON_CMD\"
  echo '[OK] Installed user crontab metrics agent'
fi
rm -rf /tmp/rtime-status-agent-install"
done

echo "[OK] Metrics agents installed."
