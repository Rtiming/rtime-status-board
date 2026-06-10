import { Activity, Clock, Gauge, ListChecks, RefreshCw, Server, ShieldCheck, TriangleAlert } from 'lucide-react';
import { AgentReportRow, Metric, Panel, Row } from '../../shared/components';
import { formatBytes, formatCount, formatDuration, formatEventKindCounts, formatHeadroomCell, formatLatencyMS, formatList, formatOpsIssue, formatSeconds, formatStatusCountSummary, formatThresholdRate, formatTime } from '../../shared/format';
import { dictionary, type Lang } from '../../shared/i18n';
import { StatusPill, statusLabel } from '../../shared/status';
import type { AgentNodeDiagnostic, DiagnosticsResponse, RuntimeEndpointStatus } from '../../types';

export function Diagnostics({
  diagnostics,
  error,
  loading,
  lang,
  onRefresh
}: {
  diagnostics: DiagnosticsResponse | null;
  error: string;
  loading: boolean;
  lang: Lang;
  onRefresh: () => void;
}) {
  const t = dictionary[lang];
  if (!diagnostics) {
    return (
      <section className="stack">
        {error && (
          <div className="notice">
            <TriangleAlert size={18} />
            <span>{error}</span>
          </div>
        )}
        <div className="loading-panel">{loading ? t.diagnosticsLoading : t.diagnosticsLoading}</div>
      </section>
    );
  }

  const serviceResourceBudgets = diagnostics.metrics.service_resource_budgets ?? [];
  const serviceResourceIssues = diagnostics.metrics.service_resource_issues ?? [];
  const collectorSummary = diagnostics.metrics.collector_summary ?? [];
  const runtime = diagnostics.runtime;
  const store = runtime?.store;
  const requests = runtime?.requests;
  const deployment = diagnostics.deployment;
  const projectDiagnostics = diagnostics.projects ?? [];
  const opsIssues = diagnostics.ops?.issues ?? [];
  const opsCounts = diagnostics.ops?.counts ?? { error: 0, warn: 0, info: 0 };
  const projectImpacts = diagnostics.ops?.project_impacts ?? [];
  const statusVolatility = diagnostics.ops?.status_volatility;
  const volatileSubjects = statusVolatility?.subjects ?? [];
  const resourceThresholds = diagnostics.ops?.resource_thresholds ?? [];
  const resourceStates = diagnostics.ops?.resource_states ?? [];
  const eventLog = diagnostics.event_log;
  const statusLogEvents = eventLog?.events ?? [];
  const agentHealth = diagnostics.agent_health ?? [];
  const opsIssueCount = opsCounts.error + opsCounts.warn + opsCounts.info;

  return (
    <section className="stack">
      {error && (
        <div className="notice">
          <TriangleAlert size={18} />
          <span>{error}</span>
        </div>
      )}
      <div className="diagnostics-head">
        <div className="metrics-grid diagnostics-summary">
          <Metric icon={ShieldCheck} label={t.overall} value={statusLabel[lang][diagnostics.overall]} status={diagnostics.overall} />
          <Metric icon={Server} label={t.rawChecks} value={String(diagnostics.checks.length)} />
          <Metric icon={Gauge} label={t.reporting} value={`${diagnostics.metrics.reporting_nodes.length}/${diagnostics.metrics.expected_nodes.length}`} />
          <Metric icon={TriangleAlert} label={t.failingChecks} value={String(diagnostics.failures.length)} />
          <Metric icon={ListChecks} label={t.opsIssues} value={String(opsIssueCount)} status={opsCounts.error > 0 ? 'down' : opsCounts.warn > 0 ? 'degraded' : 'ok'} />
        </div>
        <button className="text-button" onClick={onRefresh} type="button">
          <RefreshCw className={loading ? 'spin' : ''} size={16} />
          <span>{t.refreshDiagnostics}</span>
        </button>
      </div>

      <Panel title={t.providers}>
        <div className="provider-grid">
          {diagnostics.providers.map((provider) => (
            <article className="provider-card" key={provider.name}>
              <div className="card-head">
                <h2>{provider.name}</h2>
                <StatusPill status={provider.status} lang={lang} />
              </div>
              <p>{provider.detail}</p>
              {provider.latency_ms > 0 && <span>{provider.latency_ms}ms</span>}
            </article>
          ))}
        </div>
      </Panel>

      {deployment && (
        <Panel title={t.deploymentBoundary}>
          <div className="diag-kv compact-kv">
            <span>{t.status}</span>
            <strong>{statusLabel[lang][deployment.status]}</strong>
            <span>{t.deploymentMode}</span>
            <strong>{deployment.mode || '-'}</strong>
            <span>{t.detail}</span>
            <strong>{deployment.detail || '-'}</strong>
          </div>
          <h3 className="panel-subtitle">{t.deploymentChecks}</h3>
          <div className="inline-table deployment-table">
            <table>
              <thead>
                <tr>
                  <th>{t.category}</th>
                  <th>{t.checks}</th>
                  <th>{t.status}</th>
                  <th>{t.expected}</th>
                  <th>{t.actual}</th>
                  <th>{t.detail}</th>
                </tr>
              </thead>
              <tbody>
                {deployment.checks.map((check) => (
                  <tr key={check.key}>
                    <td>{check.category}</td>
                    <td>
                      <strong>{check.key}</strong>
                    </td>
                    <td><StatusPill status={check.status} lang={lang} /></td>
                    <td>{check.expected || '-'}</td>
                    <td>{check.actual || '-'}</td>
                    <td>{check.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      )}

      {projectDiagnostics.length > 0 && (
        <Panel title={t.projectCoverage}>
          <div className="inline-table project-coverage-table">
            <table>
              <thead>
                <tr>
                  <th>{t.project}</th>
                  <th>{t.status}</th>
                  <th>{t.services}</th>
                  <th>{t.endpoints}</th>
                  <th>{t.metricsCoverage}</th>
                  <th>{t.projectActivity}</th>
                  <th>{t.detail}</th>
                </tr>
              </thead>
              <tbody>
                {projectDiagnostics.map((project) => (
                  <tr key={project.project_id}>
                    <td>
                      <strong>{project.project_name}</strong>
                      <span>{project.project_id}</span>
                    </td>
                    <td><StatusPill status={project.status} lang={lang} /></td>
                    <td>
                      <strong>{project.service_count}</strong>
                      <span>{t.critical}: {project.critical_service_count}</span>
                      <span>{statusLabel[lang].down}: {project.down_service_count} · {statusLabel[lang].degraded}: {project.degraded_service_count}</span>
                    </td>
                    <td>
                      <strong>{project.endpoint_count}/{project.service_count}</strong>
                      <span>{t.missingEndpoint}: {project.missing_endpoint_count}</span>
                      <span>{t.unmapped}: {project.unmapped_service_count}</span>
                    </td>
                    <td>
                      <strong>{project.metrics_reporting_nodes.length}/{project.related_nodes.length}</strong>
                      <span>{t.nodes}: {formatList(project.related_nodes)}</span>
                      {(project.metrics_missing_nodes.length > 0 || project.metrics_stale_nodes.length > 0) && (
                        <span>{t.missing}: {formatList(project.metrics_missing_nodes)} · {t.stale}: {formatList(project.metrics_stale_nodes)}</span>
                      )}
                    </td>
                    <td>
                      <strong>{project.recent_check_count ?? 0}</strong>
                      <span>{t.projectRecentFailures}: {project.recent_failure_count ?? 0}</span>
                      <span>{t.last}: {formatTime(project.last_check_at)}</span>
                      <span>{t.projectRecentEvents}: {project.recent_event_count ?? 0} · {t.last}: {formatTime(project.last_event_at)}</span>
                    </td>
                    <td>{project.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      )}

      {eventLog && (
        <Panel title={t.statusLog}>
          <div className="metrics-grid event-log-summary">
            <Metric icon={ListChecks} label={t.total} value={formatCount(eventLog.total)} />
            <Metric icon={Activity} label={t.returned} value={formatCount(eventLog.returned)} />
            <Metric icon={Clock} label={t.latestEvent} value={formatTime(eventLog.latest_at)} />
            <Metric icon={Gauge} label={t.statusTargets} value={formatStatusCountSummary(eventLog.status_counts, lang)} />
          </div>
          <div className="diag-kv compact-kv event-log-meta">
            <span>{t.eventKinds}</span>
            <strong>{formatEventKindCounts(eventLog.kind_counts)}</strong>
          </div>
          {statusLogEvents.length === 0 ? (
            <div className="empty inline-empty">{t.noStatusEvents}</div>
          ) : (
            <div className="inline-table event-log-table">
              <table>
                <thead>
                  <tr>
                    <th>{t.kind}</th>
                    <th>{t.target}</th>
                    <th>{t.transition}</th>
                    <th>{t.updated}</th>
                    <th>{t.detail}</th>
                  </tr>
                </thead>
                <tbody>
                  {statusLogEvents.map((event) => (
                    <tr key={event.id}>
                      <td>{event.kind}</td>
                      <td>
                        <strong>{event.label}</strong>
                        <span>{event.subject_id}</span>
                      </td>
                      <td>
                        <div className="event-transition">
                          {event.from ? <StatusPill status={event.from} lang={lang} /> : <span>-</span>}
                          <span>-&gt;</span>
                          <StatusPill status={event.to} lang={lang} />
                        </div>
                      </td>
                      <td>{formatTime(event.created_at)}</td>
                      <td>{event.detail || statusLabel[lang][event.to]}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
      )}

      <Panel title={t.opsSummary}>
        <div className="diag-kv compact-kv">
          <span>{t.issueError}</span>
          <strong>{opsCounts.error ? String(opsCounts.error) : '-'}</strong>
          <span>{t.issueWarn}</span>
          <strong>{opsCounts.warn ? String(opsCounts.warn) : '-'}</strong>
          <span>{t.issueInfo}</span>
          <strong>{opsCounts.info ? String(opsCounts.info) : '-'}</strong>
        </div>
        {projectImpacts.length > 0 && (
          <>
            <h3 className="panel-subtitle">{t.projectImpacts}</h3>
            <div className="inline-table project-impact-table">
              <table>
                <thead>
                  <tr>
                    <th>{t.project}</th>
                    <th>{t.status}</th>
                    <th>{t.opsIssues}</th>
                    <th>{t.affected}</th>
                    <th>{t.issueKinds}</th>
                    <th>{t.detail}</th>
                  </tr>
                </thead>
                <tbody>
                  {projectImpacts.map((impact) => (
                    <tr key={impact.project_id}>
                      <td>
                        <strong>{impact.project_name}</strong>
                        <span>{impact.project_id}</span>
                      </td>
                      <td><StatusPill status={impact.status} lang={lang} /></td>
                      <td>
                        <strong>{impact.issue_count}</strong>
                        <span>{t.issueError}: {impact.error_count} · {t.issueWarn}: {impact.warn_count} · {t.issueInfo}: {impact.info_count}</span>
                      </td>
                      <td>
                        <span>{t.node}: {formatList(impact.affected_nodes)}</span>
                        <span>{t.service}: {formatList(impact.affected_services)}</span>
                      </td>
                      <td>{formatList(impact.issue_kinds)}</td>
                      <td>{impact.detail}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
        {opsIssues.length === 0 ? (
          <div className="empty inline-empty">{t.noOpsIssues}</div>
        ) : (
          <div className="issue-list">
            {opsIssues.map((issue) => (
              <div className={`issue issue-${issue.severity}`} key={`${issue.kind}-${issue.subject_id}-${issue.metric || 'state'}-${issue.observed_at || ''}`}>
                <strong>{issue.severity}</strong>
                <span>{issue.kind}</span>
                <span>{issue.subject_name || issue.subject_id}</span>
                <em>{formatOpsIssue(issue)}</em>
              </div>
            ))}
          </div>
        )}
        {volatileSubjects.length > 0 && (
          <>
            <h3 className="panel-subtitle">{t.statusVolatility}</h3>
            <div className="diag-kv compact-kv project-history-meta">
              <span>{t.window}</span>
              <strong>{formatSeconds(statusVolatility?.window_seconds ?? 0)}</strong>
              <span>{t.changeThreshold}</span>
              <strong>{formatCount(statusVolatility?.change_threshold ?? 0)}</strong>
            </div>
            <div className="inline-table status-volatility-table">
              <table>
                <thead>
                  <tr>
                    <th>{t.kind}</th>
                    <th>{t.target}</th>
                    <th>{t.statusChanges}</th>
                    <th>{t.transition}</th>
                    <th>{t.updated}</th>
                    <th>{t.detail}</th>
                  </tr>
                </thead>
                <tbody>
                  {volatileSubjects.map((subject) => (
                    <tr key={`${subject.kind}-${subject.subject_id}`}>
                      <td>{subject.kind}</td>
                      <td>
                        <strong>{subject.label || subject.subject_id}</strong>
                        <span>{subject.subject_id}</span>
                      </td>
                      <td>
                        <strong>{formatCount(subject.change_count)}</strong>
                        <StatusPill status={subject.status} lang={lang} />
                      </td>
                      <td>
                        <div className="event-transition">
                          {subject.latest_from ? <StatusPill status={subject.latest_from} lang={lang} /> : <span>-</span>}
                          <span>-&gt;</span>
                          <StatusPill status={subject.latest_to} lang={lang} />
                        </div>
                      </td>
                      <td>{formatTime(subject.latest_at)}</td>
                      <td>
                        {subject.detail}
                        {subject.latest_detail && <span>{subject.latest_detail}</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
        {resourceStates.length > 0 && (
          <>
            <h3 className="panel-subtitle">{t.resourceHeadroom}</h3>
            <div className="inline-table headroom-table">
              <table>
                <thead>
                  <tr>
                    <th>{t.node}</th>
                    <th>{t.status}</th>
                    <th>{t.cpu}</th>
                    <th>{t.memory}</th>
                    <th>{t.disk}</th>
                    <th>{t.gpu}</th>
                    <th>{t.rx}</th>
                    <th>{t.tx}</th>
                    <th>{t.read}</th>
                    <th>{t.write}</th>
                    <th>{t.updated}</th>
                  </tr>
                </thead>
                <tbody>
                  {resourceStates.map((state) => (
                    <tr key={state.node_id}>
                      <td>
                        <strong>{state.node_id}</strong>
                        <span>{state.detail}</span>
                      </td>
                      <td><StatusPill status={state.status} lang={lang} /></td>
                      <td>{formatHeadroomCell(state.cpu)}</td>
                      <td>{formatHeadroomCell(state.memory)}</td>
                      <td>{formatHeadroomCell(state.root_disk)}</td>
                      <td>{state.gpu_available ? formatHeadroomCell(state.gpu) : '-'}</td>
                      <td>{formatHeadroomCell(state.network_rx)}</td>
                      <td>{formatHeadroomCell(state.network_tx)}</td>
                      <td>{formatHeadroomCell(state.storage_read)}</td>
                      <td>{formatHeadroomCell(state.storage_write)}</td>
                      <td>{formatTime(state.observed_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
        {resourceThresholds.length > 0 && (
          <>
            <h3 className="panel-subtitle">{t.resourceThresholds}</h3>
            <div className="inline-table threshold-table">
              <table>
                <thead>
                  <tr>
                    <th>{t.node}</th>
                    <th>{t.cpu}</th>
                    <th>{t.memory}</th>
                    <th>{t.disk}</th>
                    <th>{t.gpu}</th>
                    <th>{t.rx}</th>
                    <th>{t.tx}</th>
                    <th>{t.read}</th>
                    <th>{t.write}</th>
                  </tr>
                </thead>
                <tbody>
                  {resourceThresholds.map((threshold) => (
                    <tr key={threshold.node_id}>
                      <td>{threshold.node_id}</td>
                      <td>{threshold.cpu_percent.toFixed(0)}%</td>
                      <td>{threshold.memory_percent.toFixed(0)}%</td>
                      <td>{threshold.root_disk_percent.toFixed(0)}%</td>
                      <td>{threshold.gpu_util_percent.toFixed(0)}%</td>
                      <td>{formatThresholdRate(threshold.network_rx_bps)}</td>
                      <td>{formatThresholdRate(threshold.network_tx_bps)}</td>
                      <td>{formatThresholdRate(threshold.storage_read_bps)}</td>
                      <td>{formatThresholdRate(threshold.storage_write_bps)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </Panel>

      {runtime && store && (
        <Panel title={t.runtimeState}>
          <div className="diag-kv">
            <span>{t.uptime}</span>
            <strong>{formatDuration(runtime.uptime_seconds, lang)}</strong>
            <span>{t.goVersion}</span>
            <strong>{runtime.go_version || '-'}</strong>
            <span>{t.goroutines}</span>
            <strong>{formatCount(runtime.goroutines)}</strong>
            <span>{t.buildCommit}</span>
            <strong>{runtime.build?.commit || '-'}</strong>
            <span>{t.buildTime}</span>
            <strong>{runtime.build?.built_at ? formatTime(runtime.build.built_at) : '-'}</strong>
            <span>{t.diagnosticsTotal}</span>
            <strong>{formatLatencyMS(runtime.diagnostics?.total_ms ?? 0)}</strong>
            <span>{t.heap}</span>
            <strong>{formatBytes(runtime.memory.heap_alloc_bytes)}</strong>
            <span>{t.processMemory}</span>
            <strong>{formatBytes(runtime.memory.sys_bytes)}</strong>
            <span>{t.gc}</span>
            <strong>{formatCount(runtime.memory.num_gc)}</strong>
            <span>{t.summaryCache}</span>
            <strong>{runtime.summary_cache.cached ? t.cached : '-'}</strong>
            <span>{t.cacheTTL}</span>
            <strong>{formatSeconds(runtime.summary_cache.ttl_seconds)}</strong>
            <span>{t.cacheExpiry}</span>
            <strong>{runtime.summary_cache.cached ? formatSeconds(runtime.summary_cache.seconds_until_expiry) : formatTime(runtime.summary_cache.expires_at)}</strong>
            <span>{t.sqliteStore}</span>
            <strong>{store.path || '-'}</strong>
            <span>{t.totalStore}</span>
            <strong>{formatBytes(store.total_size_bytes)}</strong>
            <span>{t.dbSize}</span>
            <strong>{formatBytes(store.db_size_bytes)}</strong>
            <span>{t.walSize}</span>
            <strong>{formatBytes(store.wal_size_bytes)}</strong>
            <span>{t.latestRows}</span>
            <strong>{formatCount(store.metrics_latest_rows)}</strong>
            <span>{t.sampleRows}</span>
            <strong>{formatCount(store.metrics_sample_rows)}</strong>
            <span>{t.reportRows}</span>
            <strong>{formatCount(store.metrics_report_log_rows)}</strong>
            <span>{t.eventRows}</span>
            <strong>{formatCount(store.event_rows)}</strong>
            <span>{t.retention}</span>
            <strong>{`${store.metrics_retention_days}d / ${store.report_log_retention_days}d`}</strong>
            <span>{t.reportLogLimit}</span>
            <strong>{formatCount(store.report_log_limit)}</strong>
            {requests && (
              <>
                <span>{t.apiRequests}</span>
                <strong>{formatCount(requests.total)}</strong>
                <span>{t.apiErrors}</span>
                <strong>{`${formatCount(requests.status_counts.client_error)} / ${formatCount(requests.status_counts.server_error)}`}</strong>
                <span>{t.apiSlowRequests}</span>
                <strong>{`${formatCount(requests.slow_count)} >= ${formatLatencyMS(requests.slow_threshold_ms)}`}</strong>
                <span>{t.apiRecentP95}</span>
                <strong>{formatLatencyMS(requests.recent_p95_duration_ms)}</strong>
              </>
            )}
          </div>
          {runtime.diagnostics?.stages?.length > 0 && (
            <>
              <h3 className="panel-subtitle">{t.diagnosticsStages}</h3>
              <div className="inline-table diagnostics-stage-table">
                <table>
                  <thead>
                    <tr>
                      <th>{t.stage}</th>
                      <th>{t.status}</th>
                      <th>{t.duration}</th>
                      <th>{t.detail}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {runtime.diagnostics.stages.map((stage) => (
                      <tr key={stage.name}>
                        <td><strong>{stage.name}</strong></td>
                        <td><StatusPill status={stage.status} lang={lang} /></td>
                        <td>{formatLatencyMS(stage.duration_ms)}</td>
                        <td>{stage.detail || '-'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
          {requests && requests.routes.length > 0 && (
            <>
              <h3 className="panel-subtitle">{t.apiRoutes}</h3>
              <div className="inline-table api-routes-table">
                <table>
                  <thead>
                    <tr>
                      <th>{t.route}</th>
                      <th>{t.total}</th>
                      <th>{t.apiErrors}</th>
                      <th>{t.avgLatency}</th>
                      <th>{t.maxLatency}</th>
                      <th>{t.lastStatus}</th>
                      <th>{t.last}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {requests.routes.slice(0, 10).map((route) => (
                      <tr key={`${route.method}-${route.route}`}>
                        <td>
                          <strong>{route.method} {route.route}</strong>
                          <span>{route.slow_count > 0 ? `${route.slow_count} ${t.apiSlowRequests}` : '-'}</span>
                        </td>
                        <td>{formatCount(route.total)}</td>
                        <td>{formatCount(route.status_counts.client_error)} / {formatCount(route.status_counts.server_error)}</td>
                        <td>{formatLatencyMS(route.avg_duration_ms)}</td>
                        <td>{formatLatencyMS(route.max_duration_ms)}</td>
                        <td>{route.last_status || '-'}</td>
                        <td>{formatTime(route.last_seen_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </Panel>
      )}

      <Panel title={t.agentHealth}>
        {agentHealth.length === 0 ? (
          <div className="empty inline-empty">{t.noMetrics}</div>
        ) : (
          <section className="table-wrap inline-table agent-health-table">
            <table>
              <thead>
                <tr>
                  <th>{t.node}</th>
                  <th>{t.status}</th>
                  <th>{t.reports}</th>
                  <th>{t.received}</th>
                  <th>{t.okFailed}</th>
                  <th>{t.lag}</th>
                  <th>{t.detail}</th>
                </tr>
              </thead>
              <tbody>
                {agentHealth.map((row) => (
                  <AgentHealthRow key={row.node_id} lang={lang} row={row} />
                ))}
              </tbody>
            </table>
          </section>
        )}
      </Panel>

      <Panel title={t.agents}>
        <div className="diag-kv">
          <span>{t.expected}</span>
          <strong>{diagnostics.metrics.expected_nodes.join(', ') || '-'}</strong>
          <span>{t.reporting}</span>
          <strong>{diagnostics.metrics.reporting_nodes.join(', ') || '-'}</strong>
          <span>{t.missing}</span>
          <strong>{diagnostics.metrics.missing_nodes.join(', ') || '-'}</strong>
          <span>{t.stale}</span>
          <strong>{diagnostics.metrics.stale_nodes.join(', ') || '-'}</strong>
          <span>{t.gpuNodes}</span>
          <strong>{diagnostics.metrics.gpu_nodes.join(', ') || '-'}</strong>
          <span>{t.collectorIssues}</span>
          <strong>{diagnostics.metrics.collector_issues.length ? String(diagnostics.metrics.collector_issues.length) : '-'}</strong>
          <span>{t.serviceResourceBudgets}</span>
          <strong>{serviceResourceBudgets.length ? `${serviceResourceBudgets.filter((budget) => budget.status === 'ok').length}/${serviceResourceBudgets.length}` : '-'}</strong>
          <span>{t.serviceResourceIssues}</span>
          <strong>{serviceResourceIssues.length ? String(serviceResourceIssues.length) : '-'}</strong>
        </div>
        {diagnostics.metrics.collector_issues.length > 0 && (
          <div className="issue-list">
            {diagnostics.metrics.collector_issues.map((issue) => (
              <div className="issue issue-warn" key={`${issue.node_id}-${issue.name}`}>
                <strong>warn</strong>
                <span>{issue.node_id}</span>
                <span>{issue.name}</span>
                <em>{issue.detail || `${issue.elapsed_ms ?? 0}ms`}</em>
              </div>
            ))}
          </div>
        )}
        {collectorSummary.length > 0 && (
          <>
            <h3 className="panel-subtitle">{t.collectorCoverage}</h3>
            <div className="inline-table collector-summary-table">
              <table>
                <thead>
                  <tr>
                    <th>{t.collectors}</th>
                    <th>{t.status}</th>
                    <th>{t.observed}</th>
                    <th>{t.okFailed}</th>
                    <th>{t.cached}</th>
                    <th>{t.missing}</th>
                    <th>{t.avgElapsed}</th>
                    <th>{t.maxElapsed}</th>
                    <th>{t.maxCacheAge}</th>
                    <th>{t.detail}</th>
                  </tr>
                </thead>
                <tbody>
                  {collectorSummary.map((collector) => (
                    <tr key={collector.name}>
                      <td><strong>{collector.name}</strong></td>
                      <td><StatusPill status={collector.status} lang={lang} /></td>
                      <td>{collector.observed_nodes}/{collector.reporting_nodes}</td>
                      <td>{collector.ok_nodes}/{collector.failed_nodes}</td>
                      <td>{collector.cached_nodes}</td>
                      <td>{formatList(collector.missing_nodes)}</td>
                      <td>{formatSeconds((collector.avg_elapsed_ms ?? 0) / 1000)}</td>
                      <td>{formatSeconds((collector.max_elapsed_ms ?? 0) / 1000)}</td>
                      <td>{collector.max_cache_age_seconds > 0 ? formatSeconds(collector.max_cache_age_seconds) : '-'}</td>
                      <td>
                        {collector.detail || '-'}
                        {(collector.failed_node_ids?.length ?? 0) > 0 && <span>{t.failedNodes}: {formatList(collector.failed_node_ids)}</span>}
                        {(collector.cached_node_ids?.length ?? 0) > 0 && <span>{t.cachedNodes}: {formatList(collector.cached_node_ids)}</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
        {serviceResourceBudgets.length > 0 && (
          <div className="issue-list">
            {serviceResourceBudgets.map((budget) => (
              <div className={`issue issue-${budget.status === 'ok' ? 'ok' : 'warn'}`} key={`${budget.node_id}-${budget.service_id}`}>
                <strong>{budget.status}</strong>
                <span>{budget.node_id}</span>
                <span>{budget.service_name}</span>
                <em>
                  {formatBytes(budget.memory_usage_bytes)}
                  {budget.max_memory_bytes ? ` / ${formatBytes(budget.max_memory_bytes)}` : ''}
                  {' · '}
                  {t.cpu} {budget.cpu_percent.toFixed(1)}%
                  {budget.max_cpu_percent ? ` / ${budget.max_cpu_percent.toFixed(1)}%` : ''}
                  {' · '}
                  {(budget.matched_containers ?? []).join(', ') || budget.detail}
                </em>
              </div>
            ))}
          </div>
        )}
        {serviceResourceIssues.length > 0 && (
          <div className="issue-list">
            {serviceResourceIssues.map((issue) => (
              <div className={`issue issue-${issue.severity}`} key={`${issue.node_id}-${issue.service_id}-${issue.metric}-${issue.container_name || 'total'}`}>
                <strong>{issue.severity}</strong>
                <span>{issue.node_id}</span>
                <span>{issue.service_name}</span>
                <em>{issue.detail}</em>
              </div>
            ))}
          </div>
        )}
      </Panel>

      <Panel title={t.recentAgentReports}>
        {diagnostics.agent_reports.length === 0 ? (
          <div className="empty inline-empty">{t.noMetrics}</div>
        ) : (
          <section className="table-wrap inline-table report-log-table">
            <table>
              <thead>
                <tr>
                  <th>{t.node}</th>
                  <th>{t.received}</th>
                  <th>{t.okFailed}</th>
                  <th>{t.schema}</th>
                  <th>{t.lag}</th>
                  <th>{t.detail}</th>
                </tr>
              </thead>
              <tbody>
                {diagnostics.agent_reports.map((report) => (
                  <AgentReportRow key={report.id} lang={lang} report={report} />
                ))}
              </tbody>
            </table>
          </section>
        )}
      </Panel>

      <Panel title={t.failingChecks}>
        {diagnostics.failures.length === 0 ? (
          <div className="empty inline-empty">{t.noFailures}</div>
        ) : (
          <div className="mini-list">
            {diagnostics.failures.map((service) => (
              <Row key={service.id} title={`${service.node_id} / ${service.name}`} subtitle={service.endpoint_key} status={service.status} meta={service.detail} />
            ))}
          </div>
        )}
      </Panel>

      <Panel title={t.configIssues}>
        {diagnostics.config.issues.length === 0 ? (
          <div className="empty inline-empty">{t.noIssues}</div>
        ) : (
          <div className="issue-list">
            {diagnostics.config.issues.map((issue) => (
              <div className={`issue issue-${issue.severity}`} key={`${issue.kind}-${issue.subject_id}`}>
                <strong>{issue.severity}</strong>
                <span>{issue.kind}</span>
                <span>{issue.subject_id}</span>
                <em>{issue.detail}</em>
              </div>
            ))}
          </div>
        )}
      </Panel>

      <section className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{t.rawChecks}</th>
              <th>{t.status}</th>
              <th>{t.latency}</th>
              <th>{t.last}</th>
              <th>{t.detail}</th>
            </tr>
          </thead>
          <tbody>
            {diagnostics.checks.map((check) => (
              <CheckRow check={check} lang={lang} key={check.key} />
            ))}
          </tbody>
        </table>
      </section>
    </section>
  );
}

function CheckRow({ check, lang }: { check: RuntimeEndpointStatus; lang: Lang }) {
  const t = dictionary[lang];
  return (
    <tr>
      <td>
        <strong>{check.group} / {check.name}</strong>
        <span>{check.key}</span>
      </td>
      <td><StatusPill status={check.status} lang={lang} /></td>
      <td>{check.response_time_ms > 0 ? `${check.response_time_ms}ms` : '-'}</td>
      <td>{formatTime(check.last_checked_at)}</td>
      <td>
        {check.detail}
        {check.recent_failures > 0 && <span>{check.recent_failures}/{check.recent_results} {t.recentFailures}</span>}
      </td>
    </tr>
  );
}

function AgentHealthRow({ row, lang }: { row: AgentNodeDiagnostic; lang: Lang }) {
  const t = dictionary[lang];
  const failedCollectors = row.latest_failed_collectors ?? [];
  return (
    <tr>
      <td>
        <strong>{row.node_id}</strong>
        <span>{row.hostname || '-'}</span>
      </td>
      <td><StatusPill status={row.status} lang={lang} /></td>
      <td>
        <strong>{row.report_count}</strong>
        <span>{t.failedReports}: {row.failed_report_count}</span>
        <span>{t.collectorFailures}: {row.collector_failure_count}</span>
      </td>
      <td>
        <strong>{formatTime(row.latest_received_at)}</strong>
        <span>{t.captured}: {formatTime(row.latest_captured_at)}</span>
      </td>
      <td>
        <strong>{row.latest_collector_ok}/{row.latest_collector_failed}</strong>
        <span>{t.schema}: {row.latest_schema_version ? `v${row.latest_schema_version}` : '-'}</span>
        <span>{t.gpu}: {row.gpu_available ? 'OK' : '-'}</span>
      </td>
      <td>{row.latest_received_at ? formatSeconds(row.latest_report_lag_seconds) : '-'}</td>
      <td>
        {row.detail}
        {failedCollectors.length > 0 && (
          <span>{failedCollectors.map((collector) => `${collector.name}: ${collector.detail || '-'}`).join('; ')}</span>
        )}
      </td>
    </tr>
  );
}
