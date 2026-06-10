# Frontend Feature Folders

Future tab/page code should live here instead of growing `App.tsx`.

Target folders:

- `overview/`
- `events/`
- `nodes/` - implemented
- `projects/` - implemented
- `services/` - implemented
- `metrics/` - implemented
- `diagnostics/` - implemented

Split order for the next frontend pass:

1. Keep `App.tsx` focused on tab/page composition and shared refresh/language
   state.
2. Treat future CPU, IO, GPU, container, process, and collector UI work as
   `features/metrics/` work.
3. Add route-like state helpers only if tab composition grows again.
4. Keep chart components behind the existing metrics history APIs.

Already split into `shared/`: i18n dictionary/startup, formatting helpers, and
status labels/pills/dots. The overview tab is split into `features/overview/`,
with its summary cards and compact node/project lists kept together. The events
timeline is split into `features/events/`. The diagnostics tab is split into
`features/diagnostics/`; the projects tab is split into `features/projects/`,
with its selector, detail panel, check history, agent reports, related metrics,
resource states, and resource history charts kept together. The nodes tab is
split into `features/nodes/`, with its node table, detail panel, check history,
Agent reports, resource states, and related projects/services kept together.
The services tab is split into `features/services/`, with its service table,
detail panel, latest check, related metrics, check history, and service events
kept together. The metrics tab is split into `features/metrics/`, with its node
selector cards, history charts, CPU/IO/network/GPU/container/process details,
and collector status kept together.
