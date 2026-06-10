# API Contract Plan

## Stable v1 APIs

The current frontend should keep calling v1 APIs:

- `GET /api/v1/summary`
- `GET /api/v1/nodes`
- `GET /api/v1/projects`
- `GET /api/v1/services`
- `GET /api/v1/metrics`
- `GET /api/v1/checks`
- `GET /api/v1/diagnostics`
- `GET /api/v1/events`

These APIs are latest-state oriented and should remain cheap.

## Future Detail APIs

Implemented detail APIs:

```text
GET /api/v1/nodes/:id/metrics?window=1h&resolution=60s
GET /api/v1/nodes/:id
GET /api/v1/nodes/:id/checks?window=24h&limit=60
GET /api/v1/projects/:id
GET /api/v1/projects/:id/checks?window=24h&limit=60
GET /api/v1/projects/:id/metrics?window=1h&limit=3000
GET /api/v1/services/:id
GET /api/v1/services/:id/checks?window=24h&limit=30
GET /api/v1/metrics/reports?node_id=:id&limit=30
GET /api/v1/telemetry/schema
```

`GET /api/v1/projects/:id` is intentionally cheap. It reuses the summary cache
and recent SQLite events, returning the project, related services, related
nodes, latest node metrics, related-node `resource_states`, project-scoped
failures, and project/service/node events. The frontend also reads
`GET /api/v1/metrics/reports?limit=100` once
and filters the bounded result to the project's related nodes, giving
project-level agent freshness and collector-failure context without adding a
new API, writer, or polling loop.

`GET /api/v1/projects/:id/checks` gives project-level troubleshooting detail.
It resolves related services through both `project.service_ids` and
`service.project_id`, then performs one on-demand Gatus recent-results read and
returns a bounded, newest-first log with service, node, endpoint, error, failed
condition, latency, and timestamp fields. It does not persist anything, does not
add a writer, and defaults to `24h` / `60`, capped at `200`.

`GET /api/v1/projects/:id/metrics` is also cheap. It resolves project-related
services through both `project.service_ids` and `service.project_id`, maps those
services to nodes, then reads the already-stored SQLite `metrics_samples` for
each node. The response contains per-node point arrays and peak summaries for
CPU, memory, disk, network rate, storage IO throughput, storage read/write
IOPS, and optional GPU utilization. It does not add a new collector, queue,
Redis dependency, or Gatus polling loop.

`GET /api/v1/nodes/:id` follows the same cheap detail pattern. It reuses the
summary cache and recent SQLite events, returning the node, related services,
related projects, latest node metrics, node-scoped `resource_states`, node-scoped failures, and
node/service/project events. The frontend also reads
`GET /api/v1/metrics/reports?node_id=:id&limit=12` on node detail pages to show
recent agent ingest logs for that node; this is a bounded SQLite read and does
not add a collector.

`GET /api/v1/nodes/:id/checks` gives node-level troubleshooting detail. It
filters the node's mapped services, performs one on-demand Gatus recent-results
read, and returns a bounded, newest-first log with service, project, endpoint,
error, failed condition, latency, and timestamp fields. It does not persist
anything, does not add a writer, and defaults to `24h` / `60`, capped at `200`.

`GET /api/v1/services/:id` is also intentionally cheap. It reuses the summary
cache and recent SQLite events, returning the service, its node, project, latest
node metrics, a latest-check summary, and service/node/project events.

`GET /api/v1/services/:id/checks` returns a bounded list derived from Gatus
recent endpoint results. It supports `window` and `limit`, defaults to
`24h` / `30`, and caps `limit` at `100`. This gives service-level debugging
detail without adding another writer or polling loop.

The `schema` endpoint should tell the frontend which domains exist per node:
CPU, memory, disk, network, IO, GPU, containers, and processes.

Implemented node history windows accept Go-style durations such as `1h` and
day windows such as `7d`, capped at 30 days.

The frontend Metrics tab now consumes `GET /api/v1/nodes/:id/metrics` directly
for node detail charts. Retention is enforced in SQLite through
`STATUS_BOARD_METRICS_RETENTION`, defaulting to `30d`.

`GET /api/v1/metrics`, `GET /api/v1/summary`, and the implemented
node/project/service detail APIs return a richer latest `metrics` object when a
v2 agent has reported:

- `schema_version`
- `cpu.per_core_percent`, `cpu.context_switches`, `cpu.interrupts`
- `storage.read_bps`, `storage.write_bps`, `storage.read_iops`,
  `storage.write_iops`, and `storage.devices`
- extended `network.interfaces` packet/error/drop counters
- `gpu.available`, `gpu.provider`, and `gpu.devices`
- `containers.available`, `containers.provider`, and top Docker container resource rows
- `processes.process_count` and top process resource rows
- `collector_status`, including optional `cached` and `cache_age_seconds`

Older v1 reports remain valid; missing v2 fields fall back to empty lists or
zero values. Container and process details are latest-state fields only; the
compact history table deliberately stores aggregate CPU/memory/disk/network/IO
throughput, storage IOPS, and GPU utilization points instead of
high-cardinality process rows.
`/api/v1/diagnostics.metrics` also includes `gpu_nodes` and
`collector_issues` for quick read-failure diagnosis, including container or
process collector failures.

`/api/v1/diagnostics.metrics.service_resource_budgets` evaluates optional
service-level budgets from static config against the latest container snapshot.
It is a read-only aggregation over already-stored metrics. It does not add a
new collector or polling loop. Each row includes service/node IDs, matched and
missing container names, current memory/CPU totals, configured limits, status,
and detail text. `service_resource_issues` contains only the rows that need
attention, such as a missing configured container or memory/CPU over budget.

`/api/v1/diagnostics.runtime` exposes status-board self-observability without a
new collector: process uptime, Go version, goroutine count, Go memory stats,
summary cache TTL/expiry, and SQLite database size/row-count/retention data.
It is intended for deployment and growth debugging, especially keeping sh-core
resource use small.

`/api/v1/diagnostics.deployment` exposes low-load deployment-boundary checks for
the board process. In production it verifies the expected localhost backend
bind (`127.0.0.1:23180`), Gatus URL (`http://127.0.0.1:23181`), runtime config
path, SQLite data path, static frontend path, frontend artifact readability,
summary cache TTL, metrics retention, SQLite size budget, configured Tailnet
entry, configured public IP entry, and public-domain DNS resolution against the
configured public IP. The public IP entry check is configuration-only;
the actual unauthenticated `401` boundary is verified by `make verify-sh-core`.
The checks use runtime settings, local file stat calls, one bounded DNS lookup,
and existing SQLite diagnostics only; they do not run Docker commands, read
Docker sockets, or start a collector.

`/api/v1/diagnostics.projects` exposes a low-load project coverage matrix. Each
row includes project ID/name/status/detail, service and critical-service counts,
down/degraded service counts, Gatus endpoint coverage, unmapped or missing
endpoint counts, related nodes, and which related nodes are reporting, missing,
or stale in the latest metrics-agent view. It also includes recent check count,
recent failure count, latest check time, recent related event count, and latest
event time. It reuses the diagnostics request's already-loaded services,
endpoint statuses, recent SQLite events, and metrics freshness data.

`/api/v1/diagnostics.event_log` embeds the latest status-change events from the
existing SQLite event table. It is bounded to the latest 20 events in the
diagnostics response and includes total event rows, returned rows, latest event
time, per-kind counts, target-status counts, and event details. It is a
read-only diagnostic view and does not write history or start another polling
loop.

`/api/v1/diagnostics.ops` is a low-load issue digest assembled from the same
latest-state sources: non-OK services, missing/stale agents, collector issues,
service resource budget issues, config warnings/errors, and latest node/GPU
threshold checks. It returns `issues`, `counts.error/warn/info`, and
`resource_thresholds` with the effective per-node CPU, memory, root disk, GPU
utilization, network RX/TX rate, and storage read/write rate limits used for
those checks. The thresholds come from `config/status-board.yaml` under
`diagnostics.resource_thresholds`; missing values inherit the defaults of CPU
90%, memory 90%, root disk 85%, GPU utilization 90%, network RX/TX 50MiB/s,
and storage read/write 100MiB/s in the current config. It also returns
`resource_states`, a per-node latest-state matrix with current value, limit,
headroom, status, stale flag, and observed time for the same resource domains.
This is an on-demand read over existing latest metrics and does not add a new
collector, writer, or polling loop. Per-node overrides are additive config only
and do not require frontend API changes. Node and project detail responses
reuse the same shape but filter it to the selected node or related project
nodes, so drill-down pages can explain resource pressure without calling the
global diagnostics endpoint.

`GET /api/v1/metrics/reports` returns recent v2 agent ingest logs. It is
bounded, newest-first, supports optional `node_id`, defaults to `limit=30`, and
caps `limit` at `200`. Each log row includes schema version, captured/received
times, report lag, collector OK/failure counts, collector status payload, GPU
presence, storage device count, and network interface count. Diagnostics embeds
the latest 20 report rows, and node detail pages request the latest 12 rows for
the selected node. Project detail pages request the latest 100 rows once and
filter them to related nodes. This lets the UI show agent freshness without
querying a separate log system.

`/api/v1/diagnostics.agent_health` is a per-node rollup over the same bounded
report rows and latest metrics freshness data. Each row includes node status,
recent report count, failed-report count, total collector failures, latest
received/captured times, latest schema, latest collector OK/failure counts, GPU
presence, device/interface counts, and latest failed collectors. It is meant for
quick collector debugging and does not add another SQLite query or polling loop.

## Metrics Report v2

Current agent endpoint:

```text
POST /api/v1/metrics/report
```

The richer endpoint is now implemented:

```text
POST /api/v1/metrics/report/v2
```

Suggested top-level payload:

```json
{
  "schema_version": 2,
  "node_id": "srv03",
  "captured_at": "2026-06-10T00:00:00Z",
  "resources": {
    "cpu": {},
    "memory": {},
    "storage": {},
    "network": {},
    "gpu": {},
    "containers": {
      "available": true,
      "provider": "docker",
      "containers": []
    },
    "processes": {
      "process_count": 42,
      "processes": []
    }
  },
  "collector_status": [
    {
      "name": "containers",
      "ok": true,
      "cached": true,
      "cache_age_seconds": 61.5,
      "elapsed_ms": 1
    }
  ]
}
```

Collector failures are reported as data. A GPU collector failing on a non-GPU
node should not make the whole report fail. When an expensive collector reuses
local cache, the cached value is still reported as latest state and the cache
age is surfaced through `collector_status`.
