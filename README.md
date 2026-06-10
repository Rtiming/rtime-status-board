# rtime-status-board

Private Tailnet status board for small personal infrastructure fleets.

## First Scope

The example configuration models five common node roles:

- `core`
- `proxy`
- `storage`
- `field`
- `gpu-edge`

The app is developed on Mac and deployed to a small Linux host with Docker
Compose. Configuration is provided through env files and YAML config;
production secrets and private topology files must stay out of git.

## Configuration Boundary

Tracked files are examples only:

- `.env.example`
- `config/status-board.example.yaml`
- `deploy/gatus/config.example.yaml`
- `deploy/nginx/status-board.example.conf`

Real local files are intentionally ignored by git:

- `.env`
- `.env.production`
- `config/status-board.yaml`
- `deploy/gatus/config.yaml`
- `deploy/nginx/status.local.conf`

Start from the examples, then replace domains, IPs, tokens, node names, and
service mappings with your own private infrastructure:

```bash
make init-env
make init-prod-env
make init-config
```

## Local Development

```bash
cp .env.example .env
make init-config
make dev
```

Local URLs:

- App API and built UI: `http://127.0.0.1:23180`
- Vite dev UI: `http://127.0.0.1:23182`
- Gatus: `http://127.0.0.1:23181`

## Tests

```bash
make test
make verify
```

`make test` is the local-only path. It uses a Go Docker image for backend checks,
so Go does not need to be installed on the Mac. Docker-Go commands are wrapped
with a timeout so a local Docker Desktop stall fails quickly instead of hanging
the whole validation. Override it with `GO_TIMEOUT_SECONDS=180 make test`. The
default Go module proxy is `GOPROXY=https://goproxy.cn,direct`, which avoids the
slow default proxy path on the China-side servers. Override it with
`GO_PROXY=<value>`.

`make verify` is the current delivery acceptance path. It removes generated
Python caches, verifies the frontend and Compose configs locally, runs backend
config/tests in a temporary sh-core Linux workspace, cleans the remote Go cache,
then runs the sh-core production acceptance check. Use `make verify-local` when
you specifically want the old local-only pipeline.

Production `make prod-up-sh-core` builds the frontend on Mac during deploy,
builds the backend artifact on the configured sh-core target with the cached Go
Docker image, then packs a small runtime image with `Dockerfile.runtime`. It
does not require Go to be installed on the Mac, and it avoids pulling
Node/Debian base images on the production host.

If local Docker-Go is unhealthy, run the Linux truth check on sh-core:

```bash
make backend-test-sh-core
```

This syncs the current workspace to a temporary directory under `/tmp`, runs the
backend config check and Go tests in a Go Docker image there, then removes the
temporary directory. It does not touch the production `/opt/rtime-status-board`
deployment. Remote Go module/build cache is kept under
`/tmp/rtime-status-board-go-cache` so repeated checks do not cold-compile every
dependency; delete it with:

```bash
CLEAN_REMOTE_GO_CACHE=1 make backend-test-sh-core
```

Remote backend checks use a longer default timeout because the first Linux Go
compile can be slow: `REMOTE_BACKEND_TIMEOUT_SECONDS=900` for the whole check
and `REMOTE_GO_STEP_TIMEOUT_SECONDS=600` for each Go container step.

To return sh-core to a clean post-check state, remove temporary backend-check
containers, workspaces, and Go cache with:

```bash
make clean-sh-core-check-cache
```

## Deploy To sh-core

```bash
make deploy-sh-core
make prod-up-sh-core
make verify-sh-core
make prod-logs-sh-core
```

`deploy-sh-core` syncs the project to `/opt/rtime-status-board`, creates
`.env.production` from `.env.example` only when missing, and verifies the remote
Docker Compose plugin. It also patches non-sensitive deployment metadata in the
remote `.env.production`, including the current Git commit and UTC build time,
so `/api/v1/diagnostics.runtime.build` can prove which revision is live. If
Compose is absent, run:

```bash
make install-compose-sh-core
```

Then rerun `make deploy-sh-core`.

`prod-up-sh-core`, `prod-ps-sh-core`, `prod-logs-sh-core`, and
`prod-down-sh-core` run Compose inside `sh-core:/opt/rtime-status-board`, using
the fixed production project name `rtime-status-board`. The `up` path reuses
`/tmp/rtime-status-board-go-cache` for Go module/build cache and expects the
frontend artifact synced by `make deploy-sh-core`.

`verify-sh-core` is the production acceptance check. It reads public/Tailnet
entry values from `.env.production` when present, otherwise falling back to the
safe example values in this public repo. It verifies the remote
production directory hygiene, remote tree size, Compose config, running
containers, status-board container resource budget, expected listening ports,
Tailnet Nginx health, public Nginx Basic Auth boundaries, health endpoint,
telemetry schema, metrics agent freshness, collector issues, recent agent
reports, per-collector coverage, all node/project/service detail endpoints, and
bounded node/project metric-history windows and node/project/service check-log
endpoints. It also verifies runtime API request diagnostics, the ops project
impact rollup contract, public HTTPS SNI/certificate routing, and scans recent
status-board container logs for fatal/error signatures. It runs `rtime-doctor`
by default. To skip
the broader rtime network doctor during a quick status-board check:

```bash
RUN_RTIME_DOCTOR=0 make verify-sh-core
```

The default production resource budget is intentionally small: `statusd` must
stay under `96MiB`, Gatus must stay under `96MiB`, combined status-board memory
must stay under `150MiB`, and combined one-shot Docker CPU must stay under
`50%`. The synced production source tree under `/opt/rtime-status-board` must
stay under `128MiB` and must not contain local artifacts such as `.git`, `.env`,
`work/`, `data/`, `node_modules`, caches, or Python bytecode. These can be
overridden for diagnosis or a future larger deployment:

```bash
MAX_STATUSD_MEM_MIB=128 \
MAX_GATUS_MEM_MIB=128 \
MAX_COMBINED_MEM_MIB=200 \
MAX_COMBINED_CPU_PERCENT=60 \
MAX_REMOTE_TREE_MIB=160 \
make verify-sh-core
```

## Production Access

Example entries:

- Tailnet: `http://100.64.10.5:18083/`
- Public IP path: `http://203.0.113.10/status-board/`
- Public domain path after DNS: `http://status.example.com/`
- Public HTTPS domain path after certificate install:
  `https://status.example.com/`

Replace these in `.env.production` and
`deploy/nginx/status.local.conf` for a real deployment.
`make deploy-sh-core` syncs only non-sensitive public/deployment metadata keys
from the ignored local `.env.production` and local Git checkout into the remote
`.env.production`:
`STATUS_BOARD_PUBLIC_DOMAIN`, `STATUS_BOARD_PUBLIC_IP`, and
`STATUS_BOARD_TAILNET_URL`, plus `STATUS_BOARD_BUILD_COMMIT` and
`STATUS_BOARD_BUILD_TIME`. Secrets such as heartbeat or agent tokens are not
copied by this path.

On sh-core, install or refresh the public HTTPS entry with:

```bash
make install-status-https-sh-core
```

The script uses acme.sh DNS-01 issuance, writes the public certificate under
`/etc/nginx/ssl`, backs up `/etc/nginx/conf.d/rtime-status-board.conf`, runs
`nginx -t`, reloads Nginx, and checks HTTPS returns `401` without credentials.

The public entries use Nginx Basic Auth. The htpasswd file lives on sh-core at
`/etc/nginx/.htpasswd-rtime-status-board` and must not be committed.
Unauthenticated public checks should return HTTP `401`; that is the expected
safe state.

If your status domain resolves to `198.18.0.0/15` from the Mac, that is usually
a local proxy fake-IP DNS answer, not your production host. Verify the
server-side public route without relying on local DNS by substituting your
actual domain and public IP:

```bash
curl --noproxy '*' --resolve status.example.com:80:203.0.113.10 \
  -I http://status.example.com/api/v1/health
curl --noproxy '*' --resolve status.example.com:443:203.0.113.10 \
  -I https://status.example.com/api/v1/health
```

Expect `401 Unauthorized` until credentials are supplied. If external DNS is
not configured yet, use the public IP path or Tailnet entry instead of the
domain.

## Metrics Agents

Install or refresh the lightweight metrics agent on the configured nodes:

```bash
./scripts/install-metrics-agents.sh
```

The installer pushes `deploy/agent/rtime-status-agent.py`, runs a one-shot
`--check` self-test on each target before installing the timer, then writes the
systemd or user-cron environment. The self-test collects the v2 payload once,
prints a redacted collector summary, and exits non-zero only when required base
collectors such as CPU, storage, or network fail. Optional GPU, container, and
process collectors are reported in the summary without requiring a GPU or
Docker socket to exist on every node. If the ignored local `.env.production`
does not contain `STATUS_BOARD_AGENT_TOKEN`, the installer reads it from
`sh-core:/opt/rtime-status-board/.env.production` by default and keeps it only
inside the install process.

The default agent remains low-load: base collectors run once per minute, while
GPU is locally cached for 120s and container/process snapshots for 300s. Tune
or disable the heavier collectors at install time without committing secrets:

```bash
STATUS_BOARD_COLLECT_CONTAINERS=0 \
STATUS_BOARD_COLLECT_PROCESSES=1 \
STATUS_BOARD_GPU_INTERVAL_SECONDS=120 \
STATUS_BOARD_CONTAINER_INTERVAL_SECONDS=300 \
STATUS_BOARD_PROCESS_INTERVAL_SECONDS=300 \
./scripts/install-metrics-agents.sh
```

For manual diagnosis on a Linux node:

```bash
STATUS_BOARD_NODE_ID=sh-core python3 /opt/rtime-status-agent/rtime-status-agent.py --check
STATUS_BOARD_NODE_ID=sh-core python3 /opt/rtime-status-agent/rtime-status-agent.py --print
```

## API

- `GET /api/v1/health`
- `GET /api/v1/summary`
- `GET /api/v1/nodes`
- `GET /api/v1/nodes/:id`
- `GET /api/v1/nodes/:id/checks?window=24h&limit=60`
- `GET /api/v1/projects`
- `GET /api/v1/projects/:id`
- `GET /api/v1/projects/:id/checks?window=24h&limit=60`
- `GET /api/v1/projects/:id/metrics?window=1h&limit=3000`
- `GET /api/v1/services`
- `GET /api/v1/services/:id`
- `GET /api/v1/services/:id/checks?window=24h&limit=30`
- `GET /api/v1/metrics`
- `GET /api/v1/metrics/reports?node_id=:id&limit=30`
- `GET /api/v1/metrics/history?node_id=:id&window=1h`
- `GET /api/v1/nodes/:id/metrics?window=1h`
- `GET /api/v1/checks`
- `GET /api/v1/diagnostics`
- `GET /api/v1/telemetry/schema`
- `POST /api/v1/metrics/report`
- `POST /api/v1/metrics/report/v2`
- `GET /api/v1/events`
- `POST /api/v1/heartbeats/:id`

`/api/v1/checks` exposes normalized Gatus endpoint results, including recent
failure counts, failed conditions, and raw error messages. `/api/v1/diagnostics`
is the reserved debug surface for future tuning: provider health, static config
mapping issues, metrics agent freshness, per-collector coverage, failing
services, and raw checks.
Node, project, and service check-log APIs also return a `summary` object over
the bounded rows, including failure counts, failure percentage, average/p95/max
latency, and latest failure time.

Backend API request logs are structured and intentionally low-detail: method,
path, status code, response bytes, and duration in milliseconds. They do not log
request bodies, query strings, or authorization tokens.
`/api/v1/diagnostics.runtime.requests` also exposes bounded in-memory API
request counters, status-class counts, recent latency percentiles, and normalized
route totals. This is for debugging and tuning only; it is not a long-retention
access log and does not write request data to SQLite.
Recent API 5xx responses and slow samples are also promoted into
`/api/v1/diagnostics.ops` as `runtime-api` issues, so interface failures appear
in the same action list as service, collector, and resource problems. Those
issues include a short normalized route summary for the recent failing or slow
samples. Slow successful `GET /api/v1/diagnostics` samples remain visible in
runtime request diagnostics but are not promoted as ops issues.

Future detail and chart APIs are documented in `docs/architecture/api-contract.md`.
`/api/v1/nodes/:id` returns a lightweight node detail view assembled from
existing summary data: node status, related services, related projects, latest
metrics, filtered resource headroom, failures, recent node/service/project
events, and a bounded recent agent report log read through
`/api/v1/metrics/reports?node_id=:id`.
`/api/v1/nodes/:id/checks` returns a bounded, newest-first node check log by
aggregating the node's mapped service endpoints from Gatus recent results. It
performs one Gatus status read on demand, does not write to SQLite, and does
not add another polling loop.
`/api/v1/nodes/:id/metrics` reuses existing SQLite samples and returns the
selected history window plus a peak summary for CPU, memory, disk, network,
storage IO throughput, storage IOPS, and optional GPU. The frontend uses this
summary for 1h/24h/7d trend cards without issuing another aggregation request.
`/api/v1/projects/:id` returns a lightweight project detail view assembled from
existing summary data: related services, nodes, latest metrics, filtered
related-node resource headroom, failures, and recent project/service/node
events. The frontend also reads the bounded
`/api/v1/metrics/reports` log once and filters it to the project's related
nodes for per-project agent freshness and collector-failure context. It does
not run extra collectors.
`/api/v1/projects/:id/checks` returns a bounded, newest-first project check log
by aggregating the project's mapped service endpoints from Gatus recent results.
It performs one Gatus status read on demand, does not write to SQLite, and does
not add another polling loop.
`/api/v1/projects/:id/metrics` reuses existing SQLite node samples and returns
per-related-node history plus peak summaries for CPU, memory, disk, network,
storage IO throughput, storage IOPS, and optional GPU. It accepts the same
capped history windows as node metrics and does not start a separate project
collector.
`/api/v1/services/:id` uses the same low-load path for a single service:
service config/status, its node, project, latest node metrics, latest check
summary, and related service/node/project events.
`/api/v1/services/:id/checks` returns a bounded recent check list from the
already-loaded Gatus endpoint results. It is intended for debugging a selected
service without introducing a separate metrics stack.
`/api/v1/metrics`, `/api/v1/summary`, and the node/project/service detail APIs
now preserve the latest v2 resource details: CPU per-core/counter fields,
storage device IO counters plus read/write IOPS, extended network interface
counters, optional GPU devices, Docker container resource snapshots, top
process snapshots, and per-collector health. Container/process fields are
latest-state only and are not written into the compact history samples.
`/api/v1/diagnostics` also lists GPU-reporting
nodes and metrics collector issues so failed reads are visible without opening
logs.
`/api/v1/diagnostics.metrics` also evaluates optional service resource budgets
against the latest container snapshots. This keeps heavier services visible
without adding another collector or polling loop.
`/api/v1/diagnostics.runtime` exposes status-board self-diagnostics: process
uptime, Go runtime memory, build commit/time, summary cache state, and SQLite
size/row-count health. It also reports Diagnostics request stage timings for
Gatus, SQLite reads, Ops rollups, deployment probes, and project/agent rollups,
so slow debug requests can be isolated without opening container logs. A total
Diagnostics request over `1500ms` or any single stage over `1000ms` is promoted
as an Ops warning unless the stage exposes a stricter local budget; deployment
live probes use their own stage budget to avoid classifying normal public-entry
network variance as a runtime fault. These values are read on demand from the
current process, environment, and database; they do not add a background
collector.
`/api/v1/diagnostics.deployment` exposes deployment-boundary checks for the
status board itself: runtime mode, localhost bind address, Gatus URL, config
path, SQLite data path, frontend artifact path, cache TTL, retention, and store
size budget. It also reports the configured Tailnet URL, public IP entry
configuration, and public-domain DNS match against the configured public IP, so
DNS/proxy mistakes are visible from the Diagnostics tab without
confusing them with the Basic Auth protected public-IP route. Production
diagnostics also probe Tailnet, public HTTP, and public HTTPS health endpoints
with short timeouts; public entries are expected to return unauthenticated
`401`. These deployment checks are cached for 30 seconds inside the process, and
transient request failures or HTTP 5xx responses get one short retry. It only
inspects already-loaded settings, local files, bounded DNS/HTTP(S) checks, and
existing SQLite diagnostics; it does not open a Docker socket, run shell
commands, or add another process.
`/api/v1/diagnostics.projects` returns a project coverage matrix assembled from
existing service config, Gatus endpoint mappings, and latest metrics-agent
freshness. It shows service counts, critical/down/degraded counts, mapped versus
missing endpoint coverage, related nodes, and metric-report coverage per
project. It also summarizes recent Gatus check rows, success/failure counts,
failure rate, mapped endpoints without recent logs, current average/max response
time, latest check time, related status-change events, and latest event time per
project. The related event summary includes event kind counts and target-status
counts, so project-level status churn can be attributed to node, service, or
project transitions without opening the raw log. It also embeds the matching
`ops.project_impacts` rollup fields
(`ops_status`, severity counts, issue kinds, affected nodes/services, and
detail) so a project row can show service-check health and operational impact
side by side. The same row now also aggregates related-node agent health from
the bounded agent report log rollup: agent status/detail, report/failure
counts, collector failure total, max report lag, lag budget/headroom, unhealthy
nodes, and GPU node count. It performs no additional checks and writes no
history.
`/api/v1/diagnostics.event_log` embeds a bounded newest-first status-change log
from SQLite. It returns total event rows, returned rows, latest event time,
kind counts, target-status counts, and the latest events. It is read-only and
reuses the existing status transition table; it does not add another collector
or polling loop.
`/api/v1/diagnostics.ops` is the action-oriented troubleshooting digest. It
aggregates existing check failures, missing/stale agents, collector issues,
service resource budget issues, config warnings, recent status volatility, and
latest resource threshold breaches. Status volatility is computed from the
existing SQLite status-transition table over the last 24h; three or more
changes for the same node/project/service become an Ops item. If the latest
transition has already returned to `ok` or `maintenance`, the item is kept as
`info` with `resolved=true`; otherwise it remains a warning. This preserves the
status-change log without keeping recovered deploy/restart noise marked as a
current degraded impact, and it adds no collector or background task. Resource
thresholds are configured in
`config/status-board.yaml` under `diagnostics.resource_thresholds`; the default
profile is CPU `90%`, memory `90%`, root disk `85%`, GPU utilization `90%`,
network RX/TX `50MiB/s`, and storage read/write `100MiB/s`. The diagnostics
payload also
returns the effective per-node thresholds so alert tuning can be checked without
opening the server config. It also returns `ops.resource_states`, a per-node
current/limit/headroom view assembled from existing latest metrics only; this is
for quick triage and does not add another collector or polling loop. Node and
project detail pages reuse the same headroom shape, filtered to the selected
node or project's related nodes.
`ops.project_impacts` groups the same issue list by affected project using
service/project ownership and node-derived project ownership. Each row includes
status, severity counts, affected nodes/services, issue kinds, and a short
detail string so project-level troubleshooting does not depend on raw Gatus
endpoint names.
`/api/v1/metrics/reports` returns a bounded recent audit log of v2 agent
reports, including receive time, schema version, collector OK/failure counts,
GPU presence, and device counts. `/api/v1/diagnostics` includes the same recent
report log plus `agent_health`, a per-node summary of recent report counts,
collector failures, latest received/captured times, schema, lag, lag budget,
lag headroom, GPU presence, and latest failed collectors. This is assembled
from the already-loaded bounded report log and metrics freshness data; it does
not add another SQLite read or collector. The Nodes detail view also consumes
the filtered report endpoint for
the selected node, so collector failures are visible without opening the global
Diagnostics tab. The Projects detail view reuses the same bounded report log
and filters it to related nodes, making project-level collector health visible
without a new API or polling loop.
The v2 report endpoint writes both latest metrics and compact SQLite history
samples for future charts. Current history points include CPU, memory, disk,
network rate, storage read/write throughput, storage read/write IOPS, and
optional GPU utilization. History windows accept Go-style durations such as
`1h` and day windows such as `7d`; windows are capped at 30 days.

Metrics history retention is controlled by `STATUS_BOARD_METRICS_RETENTION`
and defaults to `30d`. Old SQLite samples are pruned during v2 report writes so
the status board does not grow unbounded on sh-core.

## Metrics Agents

Metrics are pushed by a small Python agent on each monitored Linux node. When
root or passwordless sudo is available, it installs as a systemd timer. When a
node only allows an unprivileged SSH user, the installer falls back to a user
crontab. The agent collects CPU, load, memory, swap, root disk, uptime, and
network interface byte counters every minute. The v2 agent also collects
storage device IO, extended network counters, optional GPU readings, Docker
container snapshots, and top process snapshots. Docker absence is reported as
`provider=none` rather than a failed node; Docker permission or command failures
show up in collector diagnostics. Heavier optional collectors are cached between
agent runs: GPU refreshes every 120s by default, while container and process
snapshots refresh every 300s by default. Reports still include the latest cached
values each minute and mark cached collector rows with `cached` and
`cache_age_seconds`. Diagnostics derives cache-age budgets from those heavy
collector defaults and marks stale cached GPU/container/process rows when they
exceed that budget, without adding another collector or write path.

```bash
STATUS_BOARD_AGENT_TOKEN=<token> make deploy-sh-core
STATUS_BOARD_AGENT_TOKEN=<token> ./scripts/install-metrics-agents.sh
```

The token is stored on systemd nodes in `/etc/rtime-status-agent.env`. User-mode
fallback nodes store it under `~/.config/rtime-status-agent/env`.

Future richer collectors and retention settings are sketched in
`config/telemetry.yaml`. Production now supports the first v2 path: per-core CPU
fields, storage IO counters and IOPS history, extended network counters,
optional GPU detection, Docker container snapshots, top process snapshots,
SQLite history samples, rich latest resource detail, collector-status
diagnostics, and lightweight node/project history charts in the Metrics tab.
Recent agent report logs are capped in SQLite and intended for debugging fresh
collection issues, not as a long-retention time-series store.
Diagnostics also reports SQLite table sizes and retention settings so growth is
visible before the production host pays for it.

Useful optional agent knobs:

- `STATUS_BOARD_AGENT_CACHE_DIR=/tmp/rtime-status-agent-<uid>`
- `STATUS_BOARD_GPU_INTERVAL_SECONDS=120`
- `STATUS_BOARD_COLLECT_CONTAINERS=false`
- `STATUS_BOARD_CONTAINER_LIMIT=8`
- `STATUS_BOARD_CONTAINER_INTERVAL_SECONDS=300`
- `STATUS_BOARD_COLLECT_PROCESSES=false`
- `STATUS_BOARD_PROCESS_LIMIT=8`
- `STATUS_BOARD_PROCESS_INTERVAL_SECONDS=300`

Service-level resource budgets are configured in `config/status-board.yaml`
under a service's `resource_budget`. They match existing Docker container
snapshots by `container_names` and/or `compose_project`, then report current
memory/CPU, usage percent, and remaining headroom against budget in
Diagnostics. Example:

```yaml
resource_budget:
  container_names:
    - khoj-server-1
    - khoj-database-1
  compose_project: khoj
  max_memory_mib: 1536
  max_cpu_percent: 50
```

The production config also budgets the board's own `statusd` and `gatus`
containers under `sh-core-status-board-api`, so the UI and Ops digest can catch
status-board resource growth before it only appears in external verification
logs.

Node-level resource thresholds for the ops digest are configured separately:

```yaml
diagnostics:
  resource_thresholds:
    cpu_percent: 90
    memory_percent: 90
    root_disk_percent: 85
    gpu_util_percent: 90
    network_rx_bps: 52428800
    network_tx_bps: 52428800
    storage_read_bps: 104857600
    storage_write_bps: 104857600
    nodes:
      srv03:
        gpu_util_percent: 95
        network_tx_bps: 104857600
```

The `nodes` map is optional. Missing values inherit the global threshold, and
config validation rejects percentages outside `(0, 100]`, non-positive Bps
thresholds, or unknown node IDs.

## Project Structure

- `docs/requirements/`: product requirements and staged metric expansion.
- `docs/architecture/`: module map and API contract planning.
- `config/`: node/project/service config plus future telemetry settings.
- `backend/`: Go API, Gatus aggregation, diagnostics, SQLite persistence.
- `frontend/`: React console; future feature folders live under
  `frontend/src/features`.
- `deploy/`: Gatus, Nginx, and node agent deployment assets.
- `scripts/`: Mac-to-sh-core delivery helpers.
- `work/`: local audit notes and snapshots; excluded from deployment.

Status enum is fixed to:

- `ok`
- `degraded`
- `down`
- `unknown`
- `maintenance`
