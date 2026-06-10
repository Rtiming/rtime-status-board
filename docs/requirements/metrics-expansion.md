# Metrics Expansion Requirements

## What You Are Asking For

This status board should evolve from a simple up/down console into a private
operations cockpit for a small current node set and future projects. The key
shift is:

- from latest-only status to time-series trends;
- from generic node health to categorized resource panels;
- from service lists to drill-down pages for nodes, projects, and services;
- from CPU/memory/disk/network basics to IO, GPU, containers, and project-level
  runtime details.

In Chinese shorthand: not just "is it alive", but "why is it slow, which part is
busy, and which project is affected".

## Metric Domains

### Node Basics

- CPU total percent, per-core percent, load 1/5/15, process count.
- Memory used, available, cache/buffer, swap used.
- Root disk and important mount usage.
- Uptime and agent freshness.

### IO And Traffic

"CPU read/write" is best treated as separate IO domains:

- Disk read/write bytes per second.
- Disk read/write IOPS.
- Disk queue/latency where available.
- Network RX/TX bytes per second.
- Network packets, errors, and drops per interface.
- Optional Tailnet-specific reachability and latency.

### GPU

GPU must be optional per node. A node with no GPU should report `available:
false`, not fail collection.

Potential collectors:

- NVIDIA desktop/server: `nvidia-smi`.
- Jetson/srv03: `tegrastats`, `jtop`, or Jetson sysfs where available.
- Future fallback: report only presence/driver metadata.

Fields to reserve:

- GPU name/model, driver/runtime.
- GPU utilization percent.
- GPU memory total/used/percent.
- Temperature and power.
- Per-device list because srv03 or future machines may expose more than one
  accelerator.

### Containers And Processes

First useful scope:

- Docker container CPU/memory per status-board and selected project containers.
- Compose project status.
- Optional top processes by CPU/memory for each node.

Current implementation keeps this lightweight by reporting only the latest
bounded snapshot from the agent: Docker `ps` plus `stats --no-stream` for
containers and one `ps` scan for top processes. These rows are visible in the
Metrics detail view and existing detail APIs, but are not stored in the compact
SQLite history samples. Container/process snapshots refresh every 300s by
default and are reused from a local agent cache between runs, with cache age
surfaced in `collector_status`. The collectors can be disabled, capped, or
retuned with `STATUS_BOARD_COLLECT_CONTAINERS`,
`STATUS_BOARD_CONTAINER_LIMIT`, `STATUS_BOARD_CONTAINER_INTERVAL_SECONDS`,
`STATUS_BOARD_COLLECT_PROCESSES`, `STATUS_BOARD_PROCESS_LIMIT`, and
`STATUS_BOARD_PROCESS_INTERVAL_SECONDS`.

### Project Runtime

Projects should be able to aggregate:

- service checks from Gatus;
- node resource state from metrics agents;
- container/process state where configured;
- manual/heartbeat events.

Project detail pages should show affected services and related node/resource
signals rather than raw node dashboards only.

## Detail Pages And Charts

Target drill-down flow:

- Overview: current fleet state.
- Node detail: latest resources, charts, interfaces, disks, GPU cards,
  containers, related projects.
- Project detail: service checks, related nodes, recent events, resource impact.
- Service detail: check history, error details, linked project/node.

Current implementation status:

- Node detail charts are implemented in the Metrics tab.
- Latest metric detail now preserves v2 CPU per-core counters, storage device
  IO, network packet/error/drop counters, optional GPU devices, and collector
  health in `/api/v1/metrics` and the existing detail APIs.
- Latest metric detail now also preserves Docker container snapshots and top
  process snapshots. These are latest-only so the board can show "what is using
  this node right now" without introducing high-cardinality history writes.
- Diagnostics now exposes GPU-reporting nodes and metrics collector issues, so
  failed CPU/IO/GPU/container/process reads are visible from the board before
  opening remote logs.
- Diagnostics now also exposes a per-collector coverage matrix from latest
  agent reports. It shows observed, OK, failed, cached, and missing nodes plus
  elapsed/cache-age summaries for each reported collector, so old agents or
  partially failing collectors are visible without adding another polling loop.
  It also marks stale cached heavy collectors when cached GPU/container/process
  snapshots exceed their low-frequency cache budget, distinguishing intentional
  low-load caching from a stuck collector.
- Diagnostics now evaluates optional service resource budgets from static
  config against latest Docker container snapshots. This keeps heavy services
  such as Khoj visible without adding a new collector or long-retention metrics
  store. The production config also budgets the status board's own `statusd`
  and `gatus` containers under `sh-core-status-board-api`, so self-load is
  visible from the board rather than only from the deployment verifier. Budget
  rows expose derived memory usage percent plus memory/CPU headroom, so drill
  down views can show remaining capacity without another collector.
- Diagnostics now includes status-board runtime and SQLite store statistics
  so deployment resource growth can be checked without opening shell logs or
  adding another process.
- Backend API request logs now include method, path, status code, response
  bytes, and duration without recording bodies, query strings, or auth headers.
  Production verification also scans recent status-board container logs for
  fatal/error signatures, improving operations debugging without adding a
  collector or log store.
- Runtime diagnostics now includes bounded in-memory API request statistics:
  status-class counts, slow-request count, recent P95/max latency, latest
  samples, and normalized route totals. It gives interface debugging signals
  without adding a log store, SQLite writes, or high-cardinality raw paths.
- Runtime diagnostics now includes per-request Diagnostics stage timings for
  Gatus, SQLite reads, status volatility, Ops rollup, deployment probes, project
  diagnostics, and agent health rollup. This exposes slow debug paths without
  adding a poller, log store, or background profiler. Slow Diagnostics requests
  are promoted into Ops when total time exceeds `1500ms` or one stage exceeds
  `1000ms`.
- The ops digest now promotes recent API 5xx responses and slow API samples
  into `runtime-api` issues, so backend interface failures are visible beside
  service, collector, config, and resource problems without opening container
  logs first.
- The ops digest now includes status-volatility diagnostics from existing
  SQLite status-change events. The first version uses a fixed 24h window and a
  threshold of three changes per node/project/service, adding only one bounded
  indexed read to the Diagnostics request.
- Diagnostics now includes deployment-boundary checks for the board runtime
  itself: production mode, reserved localhost ports, Gatus URL, config/data/
  frontend paths, frontend artifact readability, cache TTL, retention, SQLite
  size budget, Tailnet URL, public IP entry configuration, and public-domain
  DNS-to-public-IP match. It also performs short health probes for Tailnet,
  public HTTP, and public HTTPS entries, expecting public entries to return
  unauthenticated `401`. These checks are on-demand local reads plus bounded
  DNS/HTTP(S) requests only, and avoid Docker socket or shell access from the app.
- Diagnostics now includes a project coverage matrix so each project can be
  checked for service count, critical/down/degraded services, Gatus endpoint
  coverage, related nodes, latest metrics-agent coverage, recent check/failure
  counts, latest check time, and recent related status-change events without
  opening individual project pages or adding checks.
- Diagnostics now includes a low-load ops issue digest that groups existing
  failures and YAML-configured threshold breaches into an action-first list for
  quick triage. The digest also exposes effective per-node resource thresholds
  and current/limit/headroom rows for CPU, memory, disk, GPU, network rate, and
  storage IO rate so alert tuning and remaining capacity are visible from the
  board itself. The same digest now includes project impact rollups with
  severity counts, affected nodes/services, and issue kinds for project-level
  troubleshooting without extra collectors.
- Diagnostics now includes a bounded status-change event log from SQLite, so
  recent service/node/project transitions are visible beside coverage and ops
  checks without adding a collector or a second log store.
- Diagnostics now also embeds recent v2 agent report logs, giving a cheap
  newest-first trail of received reports, collector OK/failure counts, lag, GPU
  presence, and device counts.
- Diagnostics also exposes `agent_health`, a per-node rollup over those bounded
  report logs. It shows recent report counts, failed reports, collector failure
  totals, latest received/captured times, schema, lag, lag budget/headroom, GPU
  presence, and latest failed collectors without adding another collector or
  polling loop.
- Project detail v1 is implemented in the Projects tab and through
  `GET /api/v1/projects/:id`; it shows related services, nodes, latest node
  metrics, failures, and recent relevant events.
- Project check logs are implemented through
  `GET /api/v1/projects/:id/checks?window=24h&limit=60`; the Projects tab now
  shows a bounded newest-first log of related service checks, errors, failed
  conditions, and latency without adding persistence or a polling loop.
- Project detail now also shows a bounded related-node Agent report view by
  reading `GET /api/v1/metrics/reports?limit=100` once and filtering to the
  project's nodes. This gives project-level collector freshness, lag, schema,
  GPU presence, and failed collector context without adding a new API,
  persistence path, or polling loop.
- Node check logs are implemented through
  `GET /api/v1/nodes/:id/checks?window=24h&limit=60`; the Nodes tab now shows a
  bounded newest-first log of that node's mapped service checks without adding
  persistence or a polling loop.
- Node detail now also shows recent per-node agent report logs through
  `GET /api/v1/metrics/reports?node_id=:id&limit=12`, making collector failures,
  report lag, schema, GPU presence, and device/interface counts visible without
  opening the global Diagnostics tab or adding another collector.
- Node and project detail responses now include filtered `resource_states`.
  These reuse the same current/limit/headroom shape as Diagnostics ops, scoped
  to the selected node or project-related nodes, so drill-down pages can explain
  CPU, memory, disk, GPU, network, and storage pressure without a second
  diagnostics fetch or another polling loop.
- Project-level metric history is implemented through
  `GET /api/v1/projects/:id/metrics?window=1h&limit=3000`; the Projects tab now
  shows per-related-node resource history and peak summaries without adding a
  separate collector.
- Node metric history now also returns a bounded `summary` over the selected
  `1h`, `24h`, or `7d` window, including sample count and peak CPU, memory,
  disk, network, storage throughput/IOPS, and GPU utilization. The Metrics tab
  uses this for trend-window peak cards without adding another query.
- Storage read/write IOPS is now part of the lightweight compact history path.
  The backend derives it from existing v2 storage device IO counters, stores
  aggregate read/write IOPS in SQLite samples, returns it from node and project
  history APIs, and the UI shows a separate storage-ops chart. This does not add
  another collector or polling loop.
- Service detail is implemented, including latest service context and bounded
  Gatus check history for a selected service.
- Node, project, and service check-log responses now include a bounded summary
  derived from the returned rows: success/failure counts, failure percentage,
  average latency, p95 latency, max latency, and latest failure time. This gives
  drill-down pages faster triage context without another collector, writer, or
  long-retention history table.

Chart windows:

- 1h for live troubleshooting.
- 24h for "what happened today".
- 7d for lightweight trend review.

SQLite is enough for v2 if samples are compact and retention is controlled.
Avoid Prometheus/VictoriaMetrics until you need high-cardinality metrics or long
retention.

## Load Budget

Keep the default agent cheap:

- base collectors every 60s;
- detailed disk/network/device collectors every 60s to 5m;
- GPU every 2m by default, using local cache between agent runs;
- container/process every 5m by default, using local cache between agent runs
  and capping rows to 8 by default;
- history retention defaults to 30d in the current build, with the option to
  lower it through `STATUS_BOARD_METRICS_RETENTION` if sh-core load or disk
  growth warrants it; downsampling can come later.
- agent report logs are short-term diagnostics only; they are capped in SQLite
  and should not become a parallel long-retention metrics store.

The sh-core target remains lightweight: no Redis, no Postgres, no heavy metric
stack in this phase.

## API Shape To Reserve

Keep existing APIs stable:

- `GET /api/v1/summary`
- `GET /api/v1/nodes`
- `GET /api/v1/projects`
- `GET /api/v1/services`
- `GET /api/v1/events`
- `GET /api/v1/metrics`
- `GET /api/v1/checks`
- `GET /api/v1/diagnostics`

Implemented or reserved detail APIs:

- `GET /api/v1/telemetry/schema`
- `GET /api/v1/nodes/:id/metrics?window=1h`
- `GET /api/v1/nodes/:id/checks?window=24h&limit=60`
- `GET /api/v1/projects/:id`
- `GET /api/v1/projects/:id/checks?window=24h&limit=60`
- `GET /api/v1/projects/:id/metrics?window=1h&limit=3000`
- `GET /api/v1/metrics/reports?node_id=:id&limit=30`
- `POST /api/v1/metrics/report/v2`

Implemented v1 detail surfaces now include `GET /api/v1/nodes/:id`,
`GET /api/v1/nodes/:id/checks`, `GET /api/v1/nodes/:id/metrics`,
`GET /api/v1/projects/:id`, `GET /api/v1/projects/:id/checks`,
`GET /api/v1/projects/:id/metrics`, `GET /api/v1/services/:id`, and
`GET /api/v1/services/:id/checks`. The service detail view deliberately stays
latest-state oriented, while the checks endpoint exposes bounded Gatus recent
results for a selected service. Node and project check logs perform one
on-demand Gatus read and project metrics history is likewise read-only over
existing SQLite samples. These detail surfaces do not add a new writer or
heavier polling pressure on sh-core.

The v2 report should be versioned because GPU/IO/history fields will expand the
payload beyond the current latest-only shape.
