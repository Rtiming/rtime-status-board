#!/usr/bin/env bash
set -euo pipefail

REMOTE_NODE="${REMOTE_NODE:-sh-core}"
STATUS_BOARD_ENV_FILE="${STATUS_BOARD_ENV_FILE:-.env.production}"
STATUS_DOMAIN="${STATUS_DOMAIN:-}"
PUBLIC_IP="${PUBLIC_IP:-}"
TAILNET_STATUS_URL="${TAILNET_STATUS_URL:-}"
RTIME_SSH="$HOME/.ai-skills/rtime-remote/scripts/rtime-ssh"

if [[ ! -x "$RTIME_SSH" ]]; then
  echo "[ERROR] rtime-ssh not found: $RTIME_SSH" >&2
  exit 1
fi

read_env_value() {
  local key="$1"
  if [[ -f "$STATUS_BOARD_ENV_FILE" ]]; then
    awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); gsub(/^"|"$/, ""); print; exit }' "$STATUS_BOARD_ENV_FILE"
  fi
}

STATUS_DOMAIN="${STATUS_DOMAIN:-$(read_env_value STATUS_BOARD_PUBLIC_DOMAIN)}"
PUBLIC_IP="${PUBLIC_IP:-$(read_env_value STATUS_BOARD_PUBLIC_IP)}"
TAILNET_STATUS_URL="${TAILNET_STATUS_URL:-$(read_env_value STATUS_BOARD_TAILNET_URL)}"

if [[ -z "$STATUS_DOMAIN" || "$STATUS_DOMAIN" == "status.example.com" ]]; then
  echo "[ERROR] STATUS_DOMAIN/STATUS_BOARD_PUBLIC_DOMAIN must be the real public status domain" >&2
  exit 2
fi
if [[ -z "$PUBLIC_IP" || "$PUBLIC_IP" == "203.0.113.10" ]]; then
  echo "[ERROR] PUBLIC_IP/STATUS_BOARD_PUBLIC_IP must be the real sh-core public IP" >&2
  exit 2
fi

remote_script="$(cat <<'REMOTE'
set -euo pipefail

DOMAIN="$STATUS_DOMAIN"
PUBLIC_IP="$PUBLIC_IP"
TAILNET_URL="$TAILNET_STATUS_URL"
ACME="/root/.acme.sh/acme.sh"
CONF="/etc/nginx/conf.d/rtime-status-board.conf"
AUTH_FILE="/etc/nginx/.htpasswd-rtime-status-board"
SSL_DIR="/etc/nginx/ssl"
CERT="$SSL_DIR/$DOMAIN.crt"
KEY="$SSL_DIR/$DOMAIN.key"
FULLCHAIN="$SSL_DIR/$DOMAIN.fullchain.crt"
ISSUE_LOG="/tmp/rtime-status-board-acme-issue.log"
INSTALL_LOG="/tmp/rtime-status-board-acme-install.log"
ACME_DIRECTORY="https://acme-v02.api.letsencrypt.org/directory"

redact_log() {
  sed -E \
    -e 's/(Secret(Key|Id)[^=[:space:]]*[=[:space:]]+)[^[:space:]]+/\1<redacted>/g' \
    -e 's/[A-Za-z0-9_-]{32,}/<redacted>/g' \
    "$1" 2>/dev/null || true
}

cert_matches_domain() {
  [[ -f "$FULLCHAIN" ]] || return 1
  openssl x509 -in "$FULLCHAIN" -noout -checkend 2592000 >/dev/null 2>&1 || return 1
  openssl x509 -in "$FULLCHAIN" -noout -ext subjectAltName 2>/dev/null | grep -Eq "DNS:${DOMAIN}([,[:space:]]|$)"
}

configure_acme_network() {
  unset http_proxy HTTP_PROXY https_proxy HTTPS_PROXY all_proxy ALL_PROXY
  if curl -fsS --connect-timeout 8 --max-time 20 "$ACME_DIRECTORY" >/dev/null; then
    echo "[REMOTE] ACME API reachable directly"
    return
  fi
  if HTTPS_PROXY=http://127.0.0.1:7890 HTTP_PROXY=http://127.0.0.1:7890 ALL_PROXY=http://127.0.0.1:7890 \
    curl -fsS --connect-timeout 8 --max-time 20 "$ACME_DIRECTORY" >/dev/null; then
    export http_proxy=http://127.0.0.1:7890
    export HTTP_PROXY=http://127.0.0.1:7890
    export https_proxy=http://127.0.0.1:7890
    export HTTPS_PROXY=http://127.0.0.1:7890
    export all_proxy=http://127.0.0.1:7890
    export ALL_PROXY=http://127.0.0.1:7890
    echo "[REMOTE] ACME API reachable through local proxy 127.0.0.1:7890"
    return
  fi
  echo "[ERROR] ACME API is not reachable directly or through local proxy 127.0.0.1:7890" >&2
  exit 1
}

echo "[REMOTE] status HTTPS install target: $DOMAIN -> $PUBLIC_IP"
if [[ ! -x "$ACME" ]]; then
  echo "[ERROR] acme.sh not found or not executable: $ACME" >&2
  exit 1
fi
if [[ ! -f "$AUTH_FILE" ]]; then
  echo "[ERROR] Basic Auth file is missing: $AUTH_FILE" >&2
  exit 1
fi
if [[ ! -f "$CONF" ]]; then
  echo "[ERROR] status-board nginx config is missing: $CONF" >&2
  exit 1
fi

mkdir -p "$SSL_DIR"

if cert_matches_domain; then
  echo "[REMOTE] existing certificate is valid for $DOMAIN"
else
  configure_acme_network
  echo "[REMOTE] issuing certificate with acme.sh dns_tencent"
  if ! "$ACME" --issue --dns dns_tencent -d "$DOMAIN" --keylength ec-256 --server letsencrypt >"$ISSUE_LOG" 2>&1; then
    echo "[ERROR] acme issue failed" >&2
    redact_log "$ISSUE_LOG" >&2
    exit 1
  fi
  if ! "$ACME" --install-cert -d "$DOMAIN" --ecc \
    --cert-file "$CERT" \
    --key-file "$KEY" \
    --fullchain-file "$FULLCHAIN" \
    --reloadcmd "systemctl reload nginx" >"$INSTALL_LOG" 2>&1; then
    echo "[ERROR] acme install-cert failed" >&2
    redact_log "$INSTALL_LOG" >&2
    exit 1
  fi
fi

if ! cert_matches_domain; then
  echo "[ERROR] installed certificate does not match $DOMAIN or expires too soon" >&2
  exit 1
fi

chmod 600 "$KEY"
chmod 644 "$CERT" "$FULLCHAIN"

tmp_conf="/tmp/rtime-status-board.nginx.conf"
backup="$CONF.bak.$(date +%Y%m%d-%H%M%S)"
python3 - <<'PY' >"$tmp_conf"
import os
from urllib.parse import urlparse

domain = os.environ["STATUS_DOMAIN"]
tailnet_url = os.environ.get("TAILNET_STATUS_URL", "")
tailnet = urlparse(tailnet_url)
tailnet_host = tailnet.hostname or "100.64.0.5"
tailnet_port = tailnet.port or 18083

print(f"""# Managed by rtime-status-board/scripts/install-status-https-sh-core.sh.
# Tailnet entry: http://{tailnet_host}:{tailnet_port}/

server {{
    listen {tailnet_host}:{tailnet_port};
    server_name {domain} rtime-status-board.local;

    allow 100.64.0.0/10;
    deny all;

    access_log /var/log/nginx/status.rtime.site.access.log;
    error_log /var/log/nginx/status.rtime.site.error.log;

    location / {{
        proxy_pass http://127.0.0.1:23180;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }}
}}

server {{
    listen 80;
    server_name {domain};

    server_tokens off;
    auth_basic "RTime Status Board";
    auth_basic_user_file /etc/nginx/.htpasswd-rtime-status-board;
    add_header X-Robots-Tag "noindex, nofollow" always;

    access_log /var/log/nginx/status.rtime.site.public.access.log;
    error_log /var/log/nginx/status.rtime.site.public.error.log;

    location / {{
        proxy_pass http://127.0.0.1:23180;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }}
}}

server {{
    listen 443 ssl http2;
    server_name {domain};

    server_tokens off;
    ssl_certificate /etc/nginx/ssl/{domain}.fullchain.crt;
    ssl_certificate_key /etc/nginx/ssl/{domain}.key;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:status_board_ssl:10m;
    ssl_session_timeout 1d;

    auth_basic "RTime Status Board";
    auth_basic_user_file /etc/nginx/.htpasswd-rtime-status-board;
    add_header X-Robots-Tag "noindex, nofollow" always;

    access_log /var/log/nginx/status.rtime.site.public-ssl.access.log;
    error_log /var/log/nginx/status.rtime.site.public-ssl.error.log;

    location / {{
        proxy_pass http://127.0.0.1:23180;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }}
}}
""")
PY

cp -p "$CONF" "$backup"
cp "$tmp_conf" "$CONF"
if ! nginx -t; then
  echo "[ERROR] nginx config test failed; restoring $backup" >&2
  cp "$backup" "$CONF"
  nginx -t >/dev/null 2>&1 || true
  exit 1
fi
systemctl reload nginx

https_status="$(curl --noproxy "*" -sS -o /tmp/rtime-status-board.https-health.html -w "%{http_code}" --resolve "$DOMAIN:443:127.0.0.1" "https://$DOMAIN/api/v1/health" || true)"
if [[ "$https_status" != "401" ]]; then
  echo "[ERROR] HTTPS unauthenticated check returned HTTP $https_status, want 401" >&2
  cat /tmp/rtime-status-board.https-health.html >&2 || true
  exit 1
fi

echo "[REMOTE] nginx config backup: $backup"
echo "[REMOTE] certificate: $FULLCHAIN"
echo "[REMOTE] HTTPS unauthenticated check: 401"
REMOTE
)"

remote_cmd="STATUS_DOMAIN=$(printf "%q" "$STATUS_DOMAIN")"
remote_cmd+=" PUBLIC_IP=$(printf "%q" "$PUBLIC_IP")"
remote_cmd+=" TAILNET_STATUS_URL=$(printf "%q" "$TAILNET_STATUS_URL")"
remote_cmd+=" bash -lc $(printf "%q" "$remote_script")"

"$RTIME_SSH" "$REMOTE_NODE" "$remote_cmd"
