# Module Map

This project should stay small, but it needs clearer boundaries before adding
IO, GPU, charts, and project drill-down pages.

## Current Runtime Layers

```text
rtime-status-board/
  backend/                 Go API, aggregation, SQLite
  frontend/                React operations console
  config/                  static nodes/projects/services plus telemetry plan
  deploy/
    agent/                 Python metrics agent and install units
    gatus/                 Gatus checks
    nginx/                 sh-core public/private entry
  scripts/                 Mac-to-sh-core delivery helpers
  docs/                    requirements, architecture, API planning
  work/                    local audit logs and snapshots, never deploy
```

## Backend Modules

Current Go package is still `backend/internal/app` to avoid a risky package
split too early. Keep the file responsibilities clear:

- `config.go`: load and validate static config.
- `types.go`: API contracts and shared DTOs.
- `gatus.go`: Gatus provider client and normalized check results.
- `aggregate.go`: summary, diagnostics, and future project/node aggregation.
- `store.go`: SQLite schema and persistence.
- `server.go`: HTTP routes only.

Future split, when the API grows:

```text
backend/internal/
  app/             server wiring
  config/          static config loader
  telemetry/       report schema, history queries, collector schema
  providers/       gatus, heartbeat, static-config, future victoriametrics
  store/           sqlite migrations and query methods
```

## Frontend Modules

Current `App.tsx` still owns the tab/page composition, but the shared UI
foundation has been split out so the next feature work does not keep growing
the root component:

```text
frontend/src/
  api.ts           fetchers and endpoint wrappers
  features/
    overview/      summary cards and compact node/project state lists
    nodes/         node table, detail, checks, agent reports, resource states
    projects/      project selector, detail, checks, agent reports, resource history
    services/      service table, detail, latest check, history, events
    metrics/       node metrics selector, history charts, resource details
    events/        status transition timeline
    diagnostics/   diagnostics tab, deployment checks, agent health, ops digest
  shared/
    format/        bytes, rates, times, durations, ops headroom text
    i18n/          Chinese/English dictionary and language startup
    status/        status labels, dots, and pills
    components/    panels, metrics, compact rows, agent report rows
  types.ts         API DTOs
```

The diagnostics tab has been split into `features/diagnostics/` because it is
the main operations/debug surface. The projects tab has also been split into
`features/projects/`, including project checks, agent report filtering, related
metrics, resource state cards, and project resource history charts. The nodes
tab has been split into `features/nodes/`, including node detail, check history,
agent report history, resource state cards, and related projects/services.
The services tab has been split into `features/services/`, including service
detail, latest check, related metrics, check history, and service events. The
metrics tab has been split into `features/metrics/`, including node selector
cards, range-driven history charts, CPU detail, IO detail, network detail, GPU,
containers, processes, and collector status. The overview tab has been split
into `features/overview/`, and the event timeline has been split into
`features/events/`. The root `App.tsx` should now stay as tab/page composition,
shared refresh/language state, and selection state only.
Keep shared formatting/status/i18n helpers in `shared/`; do not reintroduce
feature-specific display text or resource formatting into `App.tsx`.

## Agent Modules

Current agent is intentionally one Python file. Before adding GPU and IO, split
collectors conceptually:

```text
deploy/agent/
  rtime-status-agent.py
  collectors/
    cpu
    memory
    disk
    network
    gpu
    containers
    processes
```

The deployed artifact can still be one file for simplicity, but collector
functions should follow the same grouping.

## Settings Boundary

- `config/status-board.yaml`: what exists and how services/projects map.
- `config/telemetry.yaml`: what to collect, how often, and future retention.
- `.env` / `.env.production`: ports, secrets, runtime paths.
- `deploy/gatus/config.yaml`: active network/service checks.

Do not put secrets in YAML config or docs.
