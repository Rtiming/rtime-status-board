#!/usr/bin/env bash
set -euo pipefail

REMOTE_NODE="${REMOTE_NODE:-sh-core}"
REMOTE_DIR="${REMOTE_DIR:-/opt/rtime-status-board}"
RUN_RTIME_DOCTOR="${RUN_RTIME_DOCTOR:-1}"
STATUS_BOARD_ENV_FILE="${STATUS_BOARD_ENV_FILE:-.env.production}"
MAX_STATUSD_MEM_MIB="${MAX_STATUSD_MEM_MIB:-96}"
MAX_GATUS_MEM_MIB="${MAX_GATUS_MEM_MIB:-96}"
MAX_COMBINED_MEM_MIB="${MAX_COMBINED_MEM_MIB:-150}"
MAX_COMBINED_CPU_PERCENT="${MAX_COMBINED_CPU_PERCENT:-50}"
MAX_REMOTE_TREE_MIB="${MAX_REMOTE_TREE_MIB:-128}"
RECENT_LOG_WINDOW="${RECENT_LOG_WINDOW:-10m}"
RTIME_SSH="$HOME/.ai-skills/rtime-remote/scripts/rtime-ssh"
RTIME_DOCTOR="$HOME/.ai-skills/rtime-remote/scripts/rtime-doctor"

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
STATUS_DOMAIN="${STATUS_DOMAIN:-status.example.com}"
PUBLIC_IP="${PUBLIC_IP:-203.0.113.10}"
TAILNET_STATUS_URL="${TAILNET_STATUS_URL:-http://100.64.10.5:18083}"

echo "[INFO] Verifying $REMOTE_NODE:$REMOTE_DIR"
echo "[INFO] Resource budget: statusd<=${MAX_STATUSD_MEM_MIB}MiB gatus<=${MAX_GATUS_MEM_MIB}MiB combined<=${MAX_COMBINED_MEM_MIB}MiB cpu<=${MAX_COMBINED_CPU_PERCENT}%"
echo "[INFO] Remote tree budget: <=${MAX_REMOTE_TREE_MIB}MiB under $REMOTE_DIR"
echo "[INFO] Recent log window: $RECENT_LOG_WINDOW"
echo "[INFO] Public entry target: $STATUS_DOMAIN -> $PUBLIC_IP"

remote_script="$(cat <<'REMOTE'
set -euo pipefail

cd "$REMOTE_DIR"
COMPOSE="docker compose -p rtime-status-board -f compose.prod.yml --env-file .env.production"
API="http://127.0.0.1:23180"
PUBLIC_AUTH_FILE="/etc/nginx/.htpasswd-rtime-status-board"

unset http_proxy HTTP_PROXY https_proxy HTTPS_PROXY all_proxy ALL_PROXY
export no_proxy="*"
export NO_PROXY="*"

echo "[REMOTE] production directory hygiene"
python3 - <<'PY'
import os
import subprocess
from pathlib import Path

root = Path.cwd()
max_tree_mib = float(os.environ["MAX_REMOTE_TREE_MIB"])
for required in [
    "compose.prod.yml",
    "Dockerfile.runtime",
    "config/status-board.yaml",
    "deploy/gatus/config.yaml",
    ".env.production",
    "frontend/dist/index.html",
    "dist/statusd-linux-amd64",
]:
    if not (root / required).exists():
        raise SystemExit(f"missing required production file: {required}")

disallowed_paths = [
    ".git",
    ".env",
    "data",
    "work",
    "coverage",
    "tmp",
    "node_modules",
    "frontend/node_modules",
]
found = []
for rel in disallowed_paths:
    path = root / rel
    if path.exists():
        found.append(rel)

for pattern in ("__pycache__",):
    found.extend(str(path.relative_to(root)) for path in root.rglob(pattern) if path.is_dir())
found.extend(str(path.relative_to(root)) for path in root.rglob("*.pyc"))
found.extend(str(path.relative_to(root)) for path in root.rglob(".DS_Store"))

if found:
    for rel in sorted(set(found)):
        print(f"  disallowed: {rel}")
    raise SystemExit("production directory contains local/generated files")

du = subprocess.run(["du", "-sk", "."], check=True, capture_output=True, text=True)
tree_mib = int(du.stdout.split()[0]) / 1024
print(f"  remote tree: {tree_mib:.1f}MiB")
if tree_mib > max_tree_mib:
    raise SystemExit(f"remote tree {tree_mib:.1f}MiB exceeds budget {max_tree_mib:.1f}MiB")
print("  required files present; no local/generated artifacts found")
PY

echo "[REMOTE] docker compose config"
$COMPOSE config >/tmp/rtime-status-board.prod.compose.yml

echo "[REMOTE] docker compose ps"
$COMPOSE ps

echo "[REMOTE] container running state"
for container in rtime-status-board-statusd rtime-status-board-gatus; do
  running="$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null || true)"
  if [[ "$running" != "true" ]]; then
    echo "[ERROR] $container is not running" >&2
    exit 1
  fi
  echo "  $container running"
done

echo "[REMOTE] resource budget"
docker stats --no-stream --format '{{json .}}' rtime-status-board-statusd rtime-status-board-gatus >/tmp/rtime-status-board.docker-stats.jsonl
python3 - <<'PY'
import json
import os
import re
from pathlib import Path

stats_path = Path("/tmp/rtime-status-board.docker-stats.jsonl")
budgets = {
    "rtime-status-board-statusd": float(os.environ["MAX_STATUSD_MEM_MIB"]),
    "rtime-status-board-gatus": float(os.environ["MAX_GATUS_MEM_MIB"]),
}
max_combined_mem_mib = float(os.environ["MAX_COMBINED_MEM_MIB"])
max_combined_cpu_percent = float(os.environ["MAX_COMBINED_CPU_PERCENT"])

UNIT_TO_MIB = {
    "b": 1 / 1024 / 1024,
    "kb": 1000 / 1024 / 1024,
    "kib": 1 / 1024,
    "mb": 1000 / 1024,
    "mib": 1,
    "gb": 1000 * 1000 * 1000 / 1024 / 1024,
    "gib": 1024,
    "tb": 1000 * 1000 * 1000 * 1000 / 1024 / 1024,
    "tib": 1024 * 1024,
}

def parse_percent(value):
    return float(str(value).strip().rstrip("%") or 0)

def parse_mem_mib(value):
    used = str(value).split("/", 1)[0].strip()
    match = re.fullmatch(r"([0-9.]+)\s*([A-Za-z]+)", used)
    if not match:
        raise SystemExit(f"cannot parse docker stats memory value: {value!r}")
    amount = float(match.group(1))
    unit = match.group(2).lower()
    if unit not in UNIT_TO_MIB:
        raise SystemExit(f"unknown docker stats memory unit: {unit}")
    return amount * UNIT_TO_MIB[unit]

rows = []
for line in stats_path.read_text(encoding="utf-8").splitlines():
    if line.strip():
        rows.append(json.loads(line))

seen = set()
total_mem_mib = 0.0
total_cpu_percent = 0.0
for row in rows:
    name = row.get("Name") or row.get("Container")
    if name not in budgets:
        continue
    seen.add(name)
    mem_mib = parse_mem_mib(row.get("MemUsage", "0B / 0B"))
    cpu_percent = parse_percent(row.get("CPUPerc", "0%"))
    total_mem_mib += mem_mib
    total_cpu_percent += cpu_percent
    print(f"  {name}: {mem_mib:.1f}MiB CPU {cpu_percent:.2f}%")
    if mem_mib > budgets[name]:
        raise SystemExit(f"{name} memory {mem_mib:.1f}MiB exceeds budget {budgets[name]:.1f}MiB")

missing = sorted(set(budgets) - seen)
if missing:
    raise SystemExit(f"missing docker stats rows: {missing}")
if total_mem_mib > max_combined_mem_mib:
    raise SystemExit(f"combined memory {total_mem_mib:.1f}MiB exceeds budget {max_combined_mem_mib:.1f}MiB")
if total_cpu_percent > max_combined_cpu_percent:
    raise SystemExit(f"combined CPU {total_cpu_percent:.2f}% exceeds budget {max_combined_cpu_percent:.2f}%")

print(f"  combined: {total_mem_mib:.1f}MiB CPU {total_cpu_percent:.2f}%")
PY

echo "[REMOTE] listening ports"
for port in 23180 23181; do
  if ! ss -ltn | awk -v port=":$port" '$4 ~ port"$" { found = 1 } END { exit found ? 0 : 1 }'; then
    echo "[ERROR] port $port is not listening" >&2
    exit 1
  fi
  echo "  127.0.0.1:$port listening"
done

tailnet_listen="$(python3 - <<'PY'
import os
from urllib.parse import urlparse
url = os.environ.get("TAILNET_STATUS_URL", "")
parsed = urlparse(url)
if parsed.hostname and parsed.port:
    print(f"{parsed.hostname}:{parsed.port}")
PY
)"
if [[ -n "$tailnet_listen" ]] && ! ss -ltn | awk -v listen="$tailnet_listen" '$4 ~ listen"$" { found = 1 } END { exit found ? 0 : 1 }'; then
  echo "[WARN] Tailnet nginx entry $tailnet_listen was not found in ss output"
else
  echo "  ${tailnet_listen:-Tailnet nginx entry} listening"
fi

if ! ss -ltn | awk '$4 ~ /:80$/ { found = 1 } END { exit found ? 0 : 1 }'; then
  echo "[ERROR] public nginx port 80 is not listening" >&2
  exit 1
fi
echo "  public nginx port 80 listening"

if ! ss -ltn | awk '$4 ~ /:443$/ { found = 1 } END { exit found ? 0 : 1 }'; then
  echo "[ERROR] public nginx port 443 is not listening" >&2
  exit 1
fi
echo "  public nginx port 443 listening"

echo "[REMOTE] nginx entry health"
tailnet_status="$(curl --noproxy "*" -sS -o /tmp/rtime-status-board.tailnet-health.json -w "%{http_code}" "$TAILNET_STATUS_URL/api/v1/health" || true)"
if [[ "$tailnet_status" != "200" ]]; then
  echo "[ERROR] Tailnet nginx health returned HTTP $tailnet_status" >&2
  cat /tmp/rtime-status-board.tailnet-health.json >&2 || true
  exit 1
fi
echo "  Tailnet nginx health: 200"

if [[ ! -f "$PUBLIC_AUTH_FILE" ]]; then
  echo "[ERROR] public Basic Auth file is missing: $PUBLIC_AUTH_FILE" >&2
  exit 1
fi
nginx_user="$(awk '$1 == "user" { gsub(";", "", $2); print $2; exit }' /etc/nginx/nginx.conf 2>/dev/null || true)"
if [[ -n "$nginx_user" ]] && command -v runuser >/dev/null 2>&1; then
  if ! runuser -u "$nginx_user" -- test -r "$PUBLIC_AUTH_FILE"; then
    echo "[ERROR] public Basic Auth file is not readable by nginx user $nginx_user: $PUBLIC_AUTH_FILE" >&2
    exit 1
  fi
else
  if [[ ! -r "$PUBLIC_AUTH_FILE" ]]; then
    echo "[ERROR] public Basic Auth file is not readable: $PUBLIC_AUTH_FILE" >&2
    exit 1
  fi
fi
echo "  public Basic Auth file readable"

public_domain_status="$(curl --noproxy "*" -sS -o /tmp/rtime-status-board.public-domain.html -w "%{http_code}" -H "Host: $STATUS_DOMAIN" "http://127.0.0.1/api/v1/health" || true)"
if [[ "$public_domain_status" != "401" ]]; then
  echo "[ERROR] public domain entry without credentials returned HTTP $public_domain_status, want 401" >&2
  cat /tmp/rtime-status-board.public-domain.html >&2 || true
  exit 1
fi
echo "  public domain unauthenticated check: 401"

public_https_status="$(curl --noproxy "*" -sS -o /tmp/rtime-status-board.public-domain-https.html -w "%{http_code}" --resolve "$STATUS_DOMAIN:443:127.0.0.1" "https://$STATUS_DOMAIN/api/v1/health" || true)"
if [[ "$public_https_status" != "401" ]]; then
  echo "[ERROR] public HTTPS domain entry without credentials returned HTTP $public_https_status, want 401" >&2
  cat /tmp/rtime-status-board.public-domain-https.html >&2 || true
  exit 1
fi
echo "  public HTTPS domain unauthenticated check: 401"

public_ip_path_status="$(curl --noproxy "*" -sS -o /tmp/rtime-status-board.public-ip.html -w "%{http_code}" -H "Host: $PUBLIC_IP" "http://127.0.0.1/status-board/api/v1/health" || true)"
if [[ "$public_ip_path_status" != "401" ]]; then
  echo "[ERROR] public IP /status-board entry without credentials returned HTTP $public_ip_path_status, want 401" >&2
  cat /tmp/rtime-status-board.public-ip.html >&2 || true
  exit 1
fi
echo "  public IP /status-board unauthenticated check: 401"

echo "[REMOTE] API health"
curl --noproxy "*" -fsS "$API/api/v1/health" >/tmp/rtime-status-board.health.json
python3 - <<'PY'
import json
with open("/tmp/rtime-status-board.health.json", "r", encoding="utf-8") as fh:
    data = json.load(fh)
if data.get("ok") is not True:
    raise SystemExit(f"health endpoint not ok: {data}")
print("  health ok")
PY

echo "[REMOTE] API diagnostics and metrics"
python3 - <<'PY'
import json
import sys
import urllib.parse
import urllib.request

API = "http://127.0.0.1:23180"
EXPECTED_NODE_COUNT = 5

def get(path):
    with urllib.request.urlopen(API + path, timeout=10) as resp:
        return json.load(resp)

def check_history_summary(scope, subject_id, checks):
    summary = checks.get("summary") or {}
    returned = checks.get("returned", 0)
    if summary.get("total") != returned:
        raise SystemExit(f"{scope} checks summary total mismatch for {subject_id}: {summary} returned={returned}")
    for key in ("successes", "failures", "failure_percent", "avg_response_time_ms", "p95_response_time_ms", "max_response_time_ms"):
        if key not in summary:
            raise SystemExit(f"{scope} checks summary missing {key} for {subject_id}: {summary}")

def check_metrics_summary(scope, subject_id, response):
    summary = response.get("summary") or {}
    returned = response.get("returned", 0)
    if summary.get("samples") != returned:
        raise SystemExit(f"{scope} metrics summary sample mismatch for {subject_id}: {summary} returned={returned}")
    for key in ("samples", "max_cpu_percent", "max_memory_percent", "max_disk_percent", "max_network_rx_bps", "max_network_tx_bps", "max_storage_read_bps", "max_storage_write_bps", "max_storage_read_iops", "max_storage_write_iops", "gpu_available", "max_gpu_percent"):
        if key not in summary:
            raise SystemExit(f"{scope} metrics summary missing {key} for {subject_id}: {summary}")

def check_project_metrics_summary(project_id, response):
    returned = response.get("returned", 0)
    node_sample_total = 0
    for node in response.get("nodes") or []:
        summary = node.get("summary") or {}
        node_sample_total += summary.get("samples", 0)
        for key in ("samples", "max_cpu_percent", "max_memory_percent", "max_disk_percent", "max_network_rx_bps", "max_network_tx_bps", "max_storage_read_bps", "max_storage_write_bps", "max_storage_read_iops", "max_storage_write_iops", "gpu_available", "max_gpu_percent"):
            if key not in summary:
                raise SystemExit(f"project metrics summary missing {key} for {project_id}/{node.get('node_id')}: {summary}")
    if node_sample_total != returned:
        raise SystemExit(f"project metrics returned mismatch for {project_id}: node_sample_total={node_sample_total} returned={returned}")

diagnostics = get("/api/v1/diagnostics")
metrics = get("/api/v1/metrics")
schema = get("/api/v1/telemetry/schema")
nodes = get("/api/v1/nodes")
projects = get("/api/v1/projects")
services = get("/api/v1/services")

def env_value(key):
    try:
        for raw in open(".env.production", "r", encoding="utf-8"):
            line = raw.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            left, right = line.split("=", 1)
            if left.strip() == key:
                return right.strip().strip('"').strip("'")
    except FileNotFoundError:
        return ""
    return ""

deployment_diag = diagnostics.get("deployment") or {}
if deployment_diag.get("status") != "ok":
    raise SystemExit(f"deployment diagnostics not ok: {deployment_diag}")
deployment_checks = {row.get("key"): row for row in deployment_diag.get("checks") or []}
for key in ("tailnet-health", "public-http-auth", "public-https-auth", "public-domain-dns"):
    row = deployment_checks.get(key)
    if not row:
        raise SystemExit(f"deployment diagnostics missing {key}: {deployment_diag}")
    if row.get("status") != "ok":
        raise SystemExit(f"deployment diagnostic {key} is not ok: {row}")

runtime_diag = diagnostics.get("runtime") or {}
build_diag = runtime_diag.get("build") or {}
expected_build_commit = env_value("STATUS_BOARD_BUILD_COMMIT")
expected_build_time = env_value("STATUS_BOARD_BUILD_TIME")
if not build_diag.get("commit") or build_diag.get("commit") == "unknown":
    raise SystemExit(f"runtime build commit missing or unknown: {build_diag}")
if not build_diag.get("built_at") or build_diag.get("built_at") == "unknown":
    raise SystemExit(f"runtime build time missing or unknown: {build_diag}")
if expected_build_commit and expected_build_commit != "unknown" and build_diag.get("commit") != expected_build_commit:
    raise SystemExit(f"runtime build commit mismatch: diagnostics={build_diag.get('commit')} env={expected_build_commit}")
if expected_build_time and expected_build_time != "unknown" and build_diag.get("built_at") != expected_build_time:
    raise SystemExit(f"runtime build time mismatch: diagnostics={build_diag.get('built_at')} env={expected_build_time}")

runtime_timing = runtime_diag.get("diagnostics") or {}
if runtime_timing.get("total_ms", -1) < 0:
    raise SystemExit(f"runtime diagnostics timing has invalid total: {runtime_timing}")
if runtime_timing.get("total_warn_ms", 0) < 1 or runtime_timing.get("stage_warn_ms", 0) < 1:
    raise SystemExit(f"runtime diagnostics timing has invalid budgets: {runtime_timing}")
timing_stages = runtime_timing.get("stages") or []
stage_names = {stage.get("name") for stage in timing_stages}
for name in ("gatus-endpoints", "sqlite-latest-metrics", "sqlite-agent-reports", "sqlite-recent-events", "sqlite-status-volatility", "sqlite-store-diagnostics", "ops-rollup", "deployment-checks", "project-diagnostics", "agent-health-rollup"):
    if name not in stage_names:
        raise SystemExit(f"runtime diagnostics timing missing stage {name}: {timing_stages}")
for stage in timing_stages:
    for key in ("name", "status", "duration_ms", "detail"):
        if key not in stage:
            raise SystemExit(f"runtime diagnostics timing stage missing {key}: {stage}")
    if stage.get("duration_ms", -1) < 0:
        raise SystemExit(f"runtime diagnostics timing stage has invalid duration: {stage}")

request_diag = runtime_diag.get("requests") or {}
if request_diag.get("total", 0) < 1:
    raise SystemExit(f"runtime request diagnostics did not record prior API traffic: {request_diag}")
for key in ("status_counts", "slow_threshold_ms", "recent_sample_limit", "recent_p95_duration_ms", "routes"):
    if key not in request_diag:
        raise SystemExit(f"runtime request diagnostics missing {key}: {request_diag}")
if (request_diag.get("status_counts") or {}).get("success", 0) < 1:
    raise SystemExit(f"runtime request diagnostics missing successful request count: {request_diag}")
route_keys = {f"{route.get('method')} {route.get('route')}" for route in request_diag.get("routes") or []}
if "GET /api/v1/health" not in route_keys:
    raise SystemExit(f"runtime request diagnostics missing health route: {request_diag.get('routes')}")
runtime_api_issues = [issue for issue in ((diagnostics.get("ops") or {}).get("issues") or []) if issue.get("kind") == "runtime-api"]
if runtime_api_issues:
    raise SystemExit(f"runtime API request issues in healthy verification path: {runtime_api_issues}")
runtime_diagnostics_issues = [issue for issue in ((diagnostics.get("ops") or {}).get("issues") or []) if issue.get("kind") == "runtime-diagnostics"]
if runtime_diagnostics_issues:
    raise SystemExit(f"runtime diagnostics timing issues in healthy verification path: {runtime_diagnostics_issues}")
ops_diag = diagnostics.get("ops") or {}
if "project_impacts" not in ops_diag:
    raise SystemExit(f"ops diagnostics missing project_impacts: {ops_diag}")
for impact in ops_diag.get("project_impacts") or []:
    for key in ("project_id", "project_name", "status", "issue_count", "error_count", "warn_count", "info_count", "detail"):
        if key not in impact:
            raise SystemExit(f"ops project impact missing {key}: {impact}")
volatility = ops_diag.get("status_volatility") or {}
for key in ("window_seconds", "change_threshold", "subjects"):
    if key not in volatility:
        raise SystemExit(f"ops status volatility missing {key}: {volatility}")
if volatility.get("window_seconds", 0) <= 0:
    raise SystemExit(f"ops status volatility has invalid window: {volatility}")
if volatility.get("change_threshold", 0) < 1:
    raise SystemExit(f"ops status volatility has invalid threshold: {volatility}")
for subject in volatility.get("subjects") or []:
    for key in ("kind", "subject_id", "label", "change_count", "status", "latest_to", "latest_at", "detail"):
        if key not in subject:
            raise SystemExit(f"ops status volatility subject missing {key}: {subject}")

if schema.get("version") != 2:
    raise SystemExit(f"telemetry schema version = {schema.get('version')}, want 2")

metric_diag = diagnostics.get("metrics", {})
missing = metric_diag.get("missing_nodes") or []
stale = metric_diag.get("stale_nodes") or []
collector_issues = metric_diag.get("collector_issues") or []
collector_summary = metric_diag.get("collector_summary") or []
service_resource_budgets = metric_diag.get("service_resource_budgets") or []
if missing:
    raise SystemExit(f"missing metrics nodes: {missing}")
if stale:
    raise SystemExit(f"stale metrics nodes: {stale}")
if collector_issues:
    raise SystemExit(f"collector issues: {collector_issues}")
summary_by_name = {row.get("name"): row for row in collector_summary}
for name in ("gpu", "containers", "processes"):
    row = summary_by_name.get(name)
    if not row:
        raise SystemExit(f"collector summary missing {name}: {collector_summary}")
    for key in ("status", "reporting_nodes", "observed_nodes", "ok_nodes", "failed_nodes", "cached_nodes", "avg_elapsed_ms", "max_elapsed_ms"):
        if key not in row:
            raise SystemExit(f"collector summary {name} missing {key}: {row}")
    if row.get("status") != "ok":
        raise SystemExit(f"collector summary {name} is not ok: {row}")

budget_by_service = {row.get("service_id"): row for row in service_resource_budgets}
for service_id in ("orangepi-khoj", "sh-core-status-board-api"):
    row = budget_by_service.get(service_id)
    if not row:
        raise SystemExit(f"service resource budget missing {service_id}: {service_resource_budgets}")
    if row.get("status") != "ok":
        raise SystemExit(f"service resource budget {service_id} is not ok: {row}")
    if not row.get("matched_containers"):
        raise SystemExit(f"service resource budget {service_id} matched no containers: {row}")
    if row.get("max_memory_bytes", 0) > 0:
        if row.get("memory_usage_percent", -1) < 0 or row.get("memory_headroom_bytes") is None:
            raise SystemExit(f"service resource budget {service_id} missing memory headroom: {row}")
    if row.get("max_cpu_percent", 0) > 0 and row.get("cpu_headroom_percent") is None:
        raise SystemExit(f"service resource budget {service_id} missing CPU headroom: {row}")
status_board_budget = budget_by_service.get("sh-core-status-board-api") or {}
if sorted(status_board_budget.get("matched_containers") or []) != ["rtime-status-board-gatus", "rtime-status-board-statusd"]:
    raise SystemExit(f"status-board budget matched unexpected containers: {status_board_budget}")
if status_board_budget.get("memory_headroom_bytes", 0) <= 0 or status_board_budget.get("cpu_headroom_percent", 0) <= 0:
    raise SystemExit(f"status-board budget has no positive headroom: {status_board_budget}")

if len(metrics) != EXPECTED_NODE_COUNT:
    raise SystemExit(f"metrics node count = {len(metrics)}, want {EXPECTED_NODE_COUNT}")

heavy_names = {"gpu", "containers", "processes"}
cache_hits = 0
for item in metrics:
    if item.get("schema_version", 0) < 2:
        raise SystemExit(f"{item.get('node_id')} schema_version < 2")
    statuses = {entry.get("name"): entry for entry in item.get("collector_status") or []}
    missing_status = sorted(heavy_names - set(statuses))
    if missing_status:
        raise SystemExit(f"{item.get('node_id')} missing collector statuses: {missing_status}")
    cache_hits += sum(1 for name in heavy_names if statuses[name].get("cached") is True)

print(f"  diagnostics overall: {diagnostics.get('overall')}")
print(f"  reporting nodes: {len(metric_diag.get('reporting_nodes') or [])}/{len(metric_diag.get('expected_nodes') or [])}")
print(f"  metrics nodes: {len(metrics)}")
print(f"  collector summaries: {len(collector_summary)}")
print(f"  service resource budgets: {len(service_resource_budgets)}")
print(f"  cached heavy collector rows: {cache_hits}/{len(metrics) * len(heavy_names)}")
print(f"  recent agent reports: {len(diagnostics.get('agent_reports') or [])}")
print(f"  API requests observed: {request_diag.get('total')} routes={len(request_diag.get('routes') or [])}")
print(f"  build: {build_diag.get('commit')} {build_diag.get('built_at')}")
print(f"  diagnostics timing: total={runtime_timing.get('total_ms')}ms total_budget={runtime_timing.get('total_warn_ms')}ms stage_budget={runtime_timing.get('stage_warn_ms')}ms stages={len(timing_stages)}")
print(f"  status volatility rows: {len(volatility.get('subjects') or [])} threshold={volatility.get('change_threshold')}")

failures = diagnostics.get("failures") or []
if failures:
    print("  service failures:")
    for failure in failures[:10]:
        print(f"    - {failure.get('id')}: {failure.get('status')} {failure.get('detail')}")
else:
    print("  service failures: none")

print("[REMOTE] detail API smoke")
for node in nodes:
    node_id = node.get("id")
    if not node_id:
        continue
    detail = get("/api/v1/nodes/" + urllib.parse.quote(node_id, safe=""))
    if detail.get("node", {}).get("id") != node_id:
        raise SystemExit(f"node detail mismatch for {node_id}")
    if "resource_states" not in detail:
        raise SystemExit(f"node detail missing resource_states for {node_id}")
print(f"  node details: {len(nodes)}")

for project in projects:
    project_id = project.get("id")
    if not project_id:
        continue
    detail = get("/api/v1/projects/" + urllib.parse.quote(project_id, safe=""))
    if detail.get("project", {}).get("id") != project_id:
        raise SystemExit(f"project detail mismatch for {project_id}")
    if "resource_states" not in detail:
        raise SystemExit(f"project detail missing resource_states for {project_id}")
print(f"  project details: {len(projects)}")

for service in services:
    service_id = service.get("id")
    if not service_id:
        continue
    detail = get("/api/v1/services/" + urllib.parse.quote(service_id, safe=""))
    if detail.get("service", {}).get("id") != service_id:
        raise SystemExit(f"service detail mismatch for {service_id}")
    if service.get("endpoint_key") and "latest_check" not in detail:
        raise SystemExit(f"service detail missing latest_check for {service_id}")
print(f"  service details: {len(services)}")

print("[REMOTE] metrics history window smoke")
for node in nodes:
    node_id = node.get("id")
    if not node_id:
        continue
    for window in ("1h", "24h", "7d"):
        path = "/api/v1/nodes/" + urllib.parse.quote(node_id, safe="") + "/metrics?window=" + window
        history = get(path)
        check_metrics_summary("node", f"{node_id}/{window}", history)
print(f"  node metrics histories: {len(nodes)} nodes x 3 windows")

for project in projects:
    project_id = project.get("id")
    if not project_id:
        continue
    for window in ("1h", "24h", "7d"):
        path = "/api/v1/projects/" + urllib.parse.quote(project_id, safe="") + "/metrics?window=" + window + "&limit=3000"
        history = get(path)
        check_project_metrics_summary(project_id + "/" + window, history)
print(f"  project metrics histories: {len(projects)} projects x 3 windows")

print("[REMOTE] bounded check log smoke")
for node in nodes:
    node_id = node.get("id")
    if not node_id:
        continue
    path = "/api/v1/nodes/" + urllib.parse.quote(node_id, safe="") + "/checks?window=24h&limit=1"
    checks = get(path)
    if "results" not in checks:
        raise SystemExit(f"node checks missing results for {node_id}")
    check_history_summary("node", node_id, checks)
    print(f"  node {node_id}: {checks.get('returned', 0)} latest check rows")

for project in projects:
    project_id = project.get("id")
    if not project_id:
        continue
    path = "/api/v1/projects/" + urllib.parse.quote(project_id, safe="") + "/checks?window=24h&limit=1"
    checks = get(path)
    if "results" not in checks:
        raise SystemExit(f"project checks missing results for {project_id}")
    check_history_summary("project", project_id, checks)
    print(f"  project {project_id}: {checks.get('returned', 0)} latest check rows")

service_samples = [service for service in services if service.get("endpoint_key")][:5]
for service in service_samples:
    service_id = service.get("id")
    path = "/api/v1/services/" + urllib.parse.quote(service_id, safe="") + "/checks?window=24h&limit=1"
    checks = get(path)
    if "results" not in checks:
        raise SystemExit(f"service checks missing results for {service_id}")
    check_history_summary("service", service_id, checks)
    print(f"  service {service_id}: {checks.get('returned', 0)} latest check rows")
PY

echo "[REMOTE] recent container log hygiene"
for container in rtime-status-board-statusd rtime-status-board-gatus; do
  log_file="/tmp/${container}.recent.log"
  docker logs --since "$RECENT_LOG_WINDOW" --tail 300 "$container" >"$log_file" 2>&1
  if grep -Eiq '(panic|fatal|traceback|level=error|level":"error)' "$log_file"; then
    echo "[ERROR] recent $container logs contain fatal/error signatures" >&2
    grep -Ein '(panic|fatal|traceback|level=error|level":"error)' "$log_file" | tail -20 >&2
    exit 1
  fi
  line_count="$(wc -l <"$log_file" | tr -d ' ')"
  echo "  $container: ${line_count:-0} recent log lines, no fatal/error signatures"
done

echo "[REMOTE] status-board verification ok"
REMOTE
)"

remote_cmd="REMOTE_DIR=$(printf "%q" "$REMOTE_DIR")"
remote_cmd+=" STATUS_DOMAIN=$(printf "%q" "$STATUS_DOMAIN")"
remote_cmd+=" PUBLIC_IP=$(printf "%q" "$PUBLIC_IP")"
remote_cmd+=" TAILNET_STATUS_URL=$(printf "%q" "$TAILNET_STATUS_URL")"
remote_cmd+=" MAX_STATUSD_MEM_MIB=$(printf "%q" "$MAX_STATUSD_MEM_MIB")"
remote_cmd+=" MAX_GATUS_MEM_MIB=$(printf "%q" "$MAX_GATUS_MEM_MIB")"
remote_cmd+=" MAX_COMBINED_MEM_MIB=$(printf "%q" "$MAX_COMBINED_MEM_MIB")"
remote_cmd+=" MAX_COMBINED_CPU_PERCENT=$(printf "%q" "$MAX_COMBINED_CPU_PERCENT")"
remote_cmd+=" MAX_REMOTE_TREE_MIB=$(printf "%q" "$MAX_REMOTE_TREE_MIB")"
remote_cmd+=" RECENT_LOG_WINDOW=$(printf "%q" "$RECENT_LOG_WINDOW")"
remote_cmd+=" bash -lc $(printf "%q" "$remote_script")"

"$RTIME_SSH" "$REMOTE_NODE" "$remote_cmd"

if command -v dig >/dev/null 2>&1; then
  echo "[INFO] Local DNS check for $STATUS_DOMAIN"
  dns_ips="$(dig +short "$STATUS_DOMAIN" A 2>/dev/null | awk 'NF { print }' | sort -u || true)"
  if printf "%s\n" "$dns_ips" | grep -qx "$PUBLIC_IP"; then
    echo "  $STATUS_DOMAIN resolves to $PUBLIC_IP"
  elif [[ -z "$dns_ips" ]]; then
    echo "[WARN] $STATUS_DOMAIN has no local A record; use the public IP path or configure DNS before relying on the domain"
  else
    echo "[WARN] $STATUS_DOMAIN local A record is not $PUBLIC_IP: $(printf "%s" "$dns_ips" | tr '\n' ' ')"
    if printf "%s\n" "$dns_ips" | grep -Eq '^198\.18\.|^198\.19\.'; then
      echo "[WARN] 198.18.0.0/15 usually indicates a local proxy fake-IP DNS answer; test the server with curl --resolve $STATUS_DOMAIN:80:$PUBLIC_IP"
    fi
  fi
else
  echo "[WARN] dig is not installed; skipping local DNS check for $STATUS_DOMAIN"
fi

if [[ "$RUN_RTIME_DOCTOR" == "1" ]]; then
  if [[ ! -x "$RTIME_DOCTOR" ]]; then
    echo "[ERROR] rtime-doctor not found: $RTIME_DOCTOR" >&2
    exit 1
  fi
  echo "[INFO] Running rtime-doctor"
  "$RTIME_DOCTOR"
else
  echo "[INFO] Skipping rtime-doctor because RUN_RTIME_DOCTOR=$RUN_RTIME_DOCTOR"
fi

echo "[OK] sh-core production verification passed"
