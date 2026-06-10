import { useEffect, useState } from 'react';
import { Activity, Clock, Gauge, ListChecks, Server, Signal, TriangleAlert } from 'lucide-react';
import { fetchMetricReportLogs, fetchProjectChecks, fetchProjectDetail, fetchProjectMetricsHistory } from '../../api';
import { AgentReportRow, DetailResourceStatesPanel, HistoryChart, Metric, MetricSummaryCard, Row, SubPanel } from '../../shared/components';
import { formatIOPS, formatLatencyMS, formatRate, formatSeconds, formatTime } from '../../shared/format';
import { dictionary, type Lang } from '../../shared/i18n';
import { initialMetricRange, metricRanges, type MetricRange } from '../../shared/ranges';
import { StatusDot, StatusPill, statusLabel } from '../../shared/status';
import type {
  MetricsReportLogsResponse,
  ProjectCheckHistoryResponse,
  ProjectCheckResult,
  ProjectDetailResponse,
  ProjectMetricsHistoryResponse,
  ProjectNodeMetricsHistory,
  ProjectView
} from '../../types';

export function Projects({
  projects,
  lang,
  selectedProjectID,
  onSelectProject
}: {
  projects: ProjectView[];
  lang: Lang;
  selectedProjectID: string;
  onSelectProject: (projectID: string) => void;
}) {
  const t = dictionary[lang];
  const [detail, setDetail] = useState<ProjectDetailResponse | null>(null);
  const [detailError, setDetailError] = useState('');
  const [detailLoading, setDetailLoading] = useState(false);
  const selectedProject = projects.find((project) => project.id === selectedProjectID) ?? projects[0];
  const selectedID = selectedProject?.id ?? selectedProjectID;

  useEffect(() => {
    if (projects.length === 0) return;
    if (!selectedProjectID || !projects.some((project) => project.id === selectedProjectID)) {
      onSelectProject(projects[0].id);
    }
  }, [onSelectProject, projects, selectedProjectID]);

  useEffect(() => {
    if (!selectedID) {
      setDetail(null);
      return;
    }
    let cancelled = false;
    const loadProject = async () => {
      setDetailLoading(true);
      try {
        const next = await fetchProjectDetail(selectedID);
        if (!cancelled) {
          setDetail(next);
          setDetailError('');
        }
      } catch (err) {
        if (!cancelled) {
          setDetailError(err instanceof Error ? err.message : t.projectError);
        }
      } finally {
        if (!cancelled) {
          setDetailLoading(false);
        }
      }
    };
    void loadProject();
    return () => {
      cancelled = true;
    };
  }, [selectedID, t.projectError]);

  return (
    <section className="stack">
      <div className="project-selector-grid">
        {projects.map((project) => (
          <button className={project.id === selectedID ? 'project-card project-select-card active' : 'project-card project-select-card'} key={project.id} onClick={() => onSelectProject(project.id)} type="button">
            <div className="card-head">
              <h2>{project.name}</h2>
              <StatusPill status={project.status} lang={lang} />
            </div>
            <p>{project.summary}</p>
            <div className="card-foot">
              <span>{project.service_count - project.down_count}/{project.service_count} {t.checksWord}</span>
              <span>{project.detail}</span>
            </div>
          </button>
        ))}
      </div>
      {detailError && (
        <div className="notice">
          <TriangleAlert size={18} />
          <span>{detailError}</span>
        </div>
      )}
      {detailLoading && <div className="loading-panel">{t.loadingProject}</div>}
      {selectedProject && detail && <ProjectDetailPanel detail={detail} lang={lang} />}
    </section>
  );
}

function ProjectAgentReportsPanel({
  lang,
  nodeIDs,
  reports,
  reportsError,
  reportsLoading
}: {
  lang: Lang;
  nodeIDs: string[];
  reports: MetricsReportLogsResponse | null;
  reportsError: string;
  reportsLoading: boolean;
}) {
  const t = dictionary[lang];
  const nodeSet = new Set(nodeIDs);
  const logs = (reports?.logs ?? []).filter((report) => nodeSet.has(report.node_id)).slice(0, 24);
  const latest = logs[0];
  const failedCount = logs.filter((report) => report.collector_failed > 0).length;
  return (
    <SubPanel title={t.projectAgentReports}>
      <div className="diag-kv compact-kv project-history-meta project-agent-report-meta">
        <span>{t.relatedNodes}</span>
        <strong>{nodeIDs.length}</strong>
        <span>{t.reports}</span>
        <strong>{logs.length}</strong>
        <span>{t.failedReports}</span>
        <strong>{failedCount}</strong>
        <span>{t.received}</span>
        <strong>{formatTime(latest?.received_at)}</strong>
        <span>{t.lag}</span>
        <strong>{latest ? formatSeconds(latest.report_lag_seconds) : '-'}</strong>
      </div>

      {reportsError && (
        <div className="notice inline-notice">
          <TriangleAlert size={18} />
          <span>{reportsError}</span>
        </div>
      )}
      {reportsLoading && <div className="loading-panel inline-empty">{t.loadingReports}</div>}
      {!reportsLoading && logs.length === 0 && <div className="empty inline-empty">{t.noAgentReports}</div>}
      {logs.length > 0 && (
        <section className="table-wrap inline-table report-log-table project-report-log-table">
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
              {logs.map((report) => (
                <AgentReportRow key={report.id} lang={lang} report={report} />
              ))}
            </tbody>
          </table>
        </section>
      )}
    </SubPanel>
  );
}

function ProjectDetailPanel({ detail, lang }: { detail: ProjectDetailResponse; lang: Lang }) {
  const t = dictionary[lang];
  const [range, setRange] = useState<MetricRange>(initialMetricRange);
  const [checkRange, setCheckRange] = useState<MetricRange>('24h');
  const [history, setHistory] = useState<ProjectMetricsHistoryResponse | null>(null);
  const [historyError, setHistoryError] = useState('');
  const [historyLoading, setHistoryLoading] = useState(false);
  const [checks, setChecks] = useState<ProjectCheckHistoryResponse | null>(null);
  const [checksError, setChecksError] = useState('');
  const [checksLoading, setChecksLoading] = useState(false);
  const [agentReports, setAgentReports] = useState<MetricsReportLogsResponse | null>(null);
  const [agentReportsError, setAgentReportsError] = useState('');
  const [agentReportsLoading, setAgentReportsLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const loadProjectHistory = async () => {
      setHistoryLoading(true);
      try {
        const next = await fetchProjectMetricsHistory(detail.project.id, range);
        if (!cancelled) {
          setHistory(next);
          setHistoryError('');
        }
      } catch (err) {
        if (!cancelled) {
          setHistoryError(err instanceof Error ? err.message : t.historyError);
        }
      } finally {
        if (!cancelled) {
          setHistoryLoading(false);
        }
      }
    };
    void loadProjectHistory();
    return () => {
      cancelled = true;
    };
  }, [detail.project.id, range, t.historyError]);

  useEffect(() => {
    let cancelled = false;
    const loadProjectChecks = async () => {
      setChecksLoading(true);
      try {
        const next = await fetchProjectChecks(detail.project.id, checkRange, 60);
        if (!cancelled) {
          setChecks(next);
          setChecksError('');
        }
      } catch (err) {
        if (!cancelled) {
          setChecksError(err instanceof Error ? err.message : t.checksError);
        }
      } finally {
        if (!cancelled) {
          setChecksLoading(false);
        }
      }
    };
    void loadProjectChecks();
    return () => {
      cancelled = true;
    };
  }, [checkRange, detail.project.id, t.checksError]);

  useEffect(() => {
    let cancelled = false;
    const loadProjectAgentReports = async () => {
      setAgentReportsLoading(true);
      try {
        const next = await fetchMetricReportLogs('', 100);
        if (!cancelled) {
          setAgentReports(next);
          setAgentReportsError('');
        }
      } catch (err) {
        if (!cancelled) {
          setAgentReportsError(err instanceof Error ? err.message : t.reportsError);
        }
      } finally {
        if (!cancelled) {
          setAgentReportsLoading(false);
        }
      }
    };
    void loadProjectAgentReports();
    return () => {
      cancelled = true;
    };
  }, [detail.project.id, t.reportsError]);

  return (
    <section className="panel detail-panel">
      <div className="detail-head">
        <div>
          <div className="eyebrow">{t.projectDetails}</div>
          <h2>{detail.project.name}</h2>
        </div>
        <StatusPill status={detail.project.status} lang={lang} />
      </div>
      <p className="project-detail-summary">{detail.project.summary}</p>

      <div className="detail-stat-grid">
        <Metric icon={ListChecks} label={t.healthy} value={`${detail.project.service_count - detail.project.down_count}/${detail.project.service_count}`} />
        <Metric icon={TriangleAlert} label={t.affected} value={String(detail.failures.length)} status={detail.failures.length > 0 ? 'degraded' : undefined} />
        <Metric icon={Server} label={t.relatedNodes} value={String(detail.nodes.length)} />
        <Metric icon={Clock} label={t.last} value={formatTime(detail.project.last_checked_at)} />
      </div>

      <div className="project-detail-grid">
        <SubPanel title={t.failingChecks}>
          {detail.failures.length === 0 ? (
            <div className="empty inline-empty">{t.noFailures}</div>
          ) : (
            <div className="mini-list">
              {detail.failures.map((service) => (
                <Row key={service.id} title={`${service.node_id} / ${service.name}`} subtitle={service.endpoint_key || service.kind} status={service.status} meta={service.detail} />
              ))}
            </div>
          )}
        </SubPanel>

        <SubPanel title={t.relatedNodes}>
          <div className="mini-list">
            {detail.nodes.map((node) => (
              <Row key={node.id} title={node.name} subtitle={node.role} status={node.status} meta={node.detail} />
            ))}
          </div>
        </SubPanel>
      </div>

      <ProjectCheckHistoryPanel
        checks={checks}
        checksError={checksError}
        checksLoading={checksLoading}
        lang={lang}
        range={checkRange}
        setRange={setCheckRange}
      />

      <ProjectAgentReportsPanel
        lang={lang}
        nodeIDs={detail.nodes.map((node) => node.id)}
        reports={agentReports}
        reportsError={agentReportsError}
        reportsLoading={agentReportsLoading}
      />

      <SubPanel title={t.relatedMetrics}>
        {detail.metrics.length === 0 ? (
          <div className="empty inline-empty">{t.noMetrics}</div>
        ) : (
          <div className="project-metrics-grid">
            {detail.metrics.map((metric) => (
              <MetricSummaryCard metric={metric} lang={lang} key={metric.node_id} />
            ))}
          </div>
        )}
      </SubPanel>

      <DetailResourceStatesPanel lang={lang} states={detail.resource_states ?? []} />

      <ProjectResourceHistoryPanel
        history={history}
        historyError={historyError}
        historyLoading={historyLoading}
        lang={lang}
        range={range}
        setRange={setRange}
      />

      <SubPanel title={t.relatedServices}>
        <div className="table-wrap inline-table">
          <table>
            <thead>
              <tr>
                <th>{t.service}</th>
                <th>{t.status}</th>
                <th>{t.node}</th>
                <th>{t.latency}</th>
                <th>{t.detail}</th>
              </tr>
            </thead>
            <tbody>
              {detail.services.map((service) => (
                <tr key={service.id}>
                  <td>
                    <strong>{service.name}</strong>
                    <span>{service.critical ? t.critical : t.standard}</span>
                  </td>
                  <td><StatusPill status={service.status} lang={lang} /></td>
                  <td>{service.node_id}</td>
                  <td>{service.response_time_ms > 0 ? `${service.response_time_ms}ms` : '-'}</td>
                  <td>{service.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </SubPanel>

      <SubPanel title={t.projectEvents}>
        {detail.events.length === 0 ? (
          <div className="empty inline-empty">{t.noProjectEvents}</div>
        ) : (
          <div className="mini-list">
            {detail.events.map((event) => (
              <Row key={event.id} title={event.label} subtitle={`${event.kind} · ${formatTime(event.created_at)}`} status={event.to} meta={event.detail || statusLabel[lang][event.to]} />
            ))}
          </div>
        )}
      </SubPanel>
    </section>
  );
}

function ProjectCheckHistoryPanel({
  checks,
  checksError,
  checksLoading,
  lang,
  range,
  setRange
}: {
  checks: ProjectCheckHistoryResponse | null;
  checksError: string;
  checksLoading: boolean;
  lang: Lang;
  range: MetricRange;
  setRange: (range: MetricRange) => void;
}) {
  const t = dictionary[lang];
  const results = checks?.results ?? [];
  const summary = checks?.summary;
  const failedCount = summary?.failures ?? results.filter((result) => !result.success).length;
  return (
    <SubPanel title={t.projectCheckHistory}>
      <div className="detail-head subpanel-head">
        <div className="diag-kv compact-kv project-history-meta">
          <span>{t.checks}</span>
          <strong>{checks?.returned ?? 0}</strong>
          <span>{t.endpoint}</span>
          <strong>{checks?.endpoint_count ?? 0}</strong>
          <span>{t.recentFailures}</span>
          <strong>{failedCount}</strong>
          <span>{t.avgLatency}</span>
          <strong>{formatLatencyMS(summary?.avg_response_time_ms)}</strong>
          <span>{t.p95Latency}</span>
          <strong>{formatLatencyMS(summary?.p95_response_time_ms)}</strong>
          <span>{t.lastFailure}</span>
          <strong>{formatTime(summary?.last_failure_at)}</strong>
        </div>
        <div className="range-control" aria-label={t.range}>
          {metricRanges.map((item) => (
            <button className={range === item ? 'range-button active' : 'range-button'} key={item} onClick={() => setRange(item)} type="button">
              {item}
            </button>
          ))}
        </div>
      </div>

      {checksError && (
        <div className="notice inline-notice">
          <TriangleAlert size={18} />
          <span>{checksError}</span>
        </div>
      )}
      {checksLoading && <div className="loading-panel inline-empty">{t.loadingChecks}</div>}
      {!checksLoading && results.length === 0 && <div className="empty inline-empty">{t.noCheckHistory}</div>}
      {results.length > 0 && (
        <div className="check-history-list">
          {results.map((result) => (
            <ProjectCheckHistoryItem key={projectCheckKey(result)} lang={lang} result={result} />
          ))}
        </div>
      )}
    </SubPanel>
  );
}

function ProjectCheckHistoryItem({ lang, result }: { lang: Lang; result: ProjectCheckResult }) {
  const t = dictionary[lang];
  return (
    <article className="check-history-item">
      <StatusDot status={result.status} />
      <div>
        <div className="check-history-head">
          <strong>{result.node_id} / {result.service_name}</strong>
          <span>{formatTime(result.timestamp)} · {result.response_time_ms > 0 ? `${result.response_time_ms}ms` : '-'}</span>
        </div>
        <p>{result.detail || statusLabel[lang][result.status]}</p>
        <span>{t.endpoint}: {result.endpoint_key || '-'}</span>
        {result.errors && result.errors.length > 0 && <span>{t.errors}: {result.errors.join('; ')}</span>}
        {result.conditions && result.conditions.length > 0 && (
          <span>
            {t.conditions}: {result.conditions.map((condition) => `${condition.success ? 'OK' : 'FAIL'} ${condition.condition}`).join('; ')}
          </span>
        )}
      </div>
    </article>
  );
}

function ProjectResourceHistoryPanel({
  history,
  historyError,
  historyLoading,
  lang,
  range,
  setRange
}: {
  history: ProjectMetricsHistoryResponse | null;
  historyError: string;
  historyLoading: boolean;
  lang: Lang;
  range: MetricRange;
  setRange: (range: MetricRange) => void;
}) {
  const t = dictionary[lang];
  const nodes = history?.nodes ?? [];
  const summary = summarizeProjectHistory(nodes);
  return (
    <SubPanel title={t.projectResourceHistory}>
      <div className="detail-head subpanel-head">
        <div className="diag-kv compact-kv project-history-meta">
          <span>{t.samples}</span>
          <strong>{history?.returned ?? 0}</strong>
          <span>{t.relatedNodes}</span>
          <strong>{nodes.length}</strong>
        </div>
        <div className="range-control" aria-label={t.range}>
          {metricRanges.map((item) => (
            <button className={range === item ? 'range-button active' : 'range-button'} key={item} onClick={() => setRange(item)} type="button">
              {item}
            </button>
          ))}
        </div>
      </div>

      {historyError && (
        <div className="notice inline-notice">
          <TriangleAlert size={18} />
          <span>{historyError}</span>
        </div>
      )}
      {historyLoading && <div className="loading-panel inline-empty">{t.loadingHistory}</div>}
      {!historyLoading && nodes.length === 0 && <div className="empty inline-empty">{t.noHistory}</div>}

      {nodes.length > 0 && (
        <>
          <div className="detail-stat-grid">
            <Metric icon={Gauge} label={t.samples} value={String(summary.samples)} />
            <Metric icon={Activity} label={`${t.peak} ${t.cpu}`} value={`${summary.maxCPU.toFixed(1)}%`} />
            <Metric icon={Signal} label={`${t.peak} ${t.network}`} value={`${formatRate(summary.maxRX)} / ${formatRate(summary.maxTX)}`} />
            <Metric icon={Server} label={`${t.peak} ${t.storageIO}`} value={`${formatRate(summary.maxRead)} / ${formatRate(summary.maxWrite)}`} />
            <Metric icon={Server} label={`${t.peak} ${t.storageOps}`} value={`${formatIOPS(summary.maxReadIOPS)} / ${formatIOPS(summary.maxWriteIOPS)}`} />
          </div>

          <div className="project-history-grid">
            {nodes.map((node) => (
              <ProjectNodeHistoryCard key={node.node_id} lang={lang} node={node} />
            ))}
          </div>
        </>
      )}
    </SubPanel>
  );
}

function ProjectNodeHistoryCard({ lang, node }: { lang: Lang; node: ProjectNodeMetricsHistory }) {
  const t = dictionary[lang];
  const summary = node.summary;
  return (
    <article className="resource-card history-node-card">
      <div className="card-head">
        <h2>{node.node_id}</h2>
        <span className="stale-label">{summary.samples} {t.samples}</span>
      </div>
      <div className="diag-kv compact-kv">
        <span>{`${t.peak} ${t.cpu}`}</span>
        <strong>{summary.max_cpu_percent.toFixed(1)}%</strong>
        <span>{`${t.peak} ${t.memory}`}</span>
        <strong>{summary.max_memory_percent.toFixed(1)}%</strong>
        <span>{`${t.peak} ${t.disk}`}</span>
        <strong>{summary.max_disk_percent.toFixed(1)}%</strong>
        <span>{`${t.peak} ${t.network}`}</span>
        <strong>{t.rx} {formatRate(summary.max_network_rx_bps)} · {t.tx} {formatRate(summary.max_network_tx_bps)}</strong>
        <span>{`${t.peak} ${t.storageIO}`}</span>
        <strong>{t.read} {formatRate(summary.max_storage_read_bps)} · {t.write} {formatRate(summary.max_storage_write_bps)}</strong>
        <span>{`${t.peak} ${t.storageOps}`}</span>
        <strong>{t.read} {formatIOPS(summary.max_storage_read_iops)} · {t.write} {formatIOPS(summary.max_storage_write_iops)}</strong>
        <span>{t.gpu}</span>
        <strong>{summary.gpu_available ? `${summary.max_gpu_percent.toFixed(1)}%` : t.noGpu}</strong>
      </div>
      {node.points.length > 1 && (
        <HistoryChart
          title={t.current}
          points={node.points}
          series={[
            { label: t.cpu, color: '#2f8f68', value: (point) => point.cpu_percent },
            { label: t.memory, color: '#3279c8', value: (point) => point.memory_percent },
            { label: t.disk, color: '#b36b2c', value: (point) => point.disk_percent },
            { label: t.gpu, color: '#7a4fb3', value: (point) => (point.gpu_available ? point.gpu_percent : 0) }
          ]}
          valueLabel={(value) => `${value.toFixed(0)}%`}
        />
      )}
      {node.points.length > 1 && (
        <HistoryChart
          title={t.storageOps}
          points={node.points}
          series={[
            { label: t.read, color: '#7a4fb3', value: (point) => point.storage_read_iops ?? 0 },
            { label: t.write, color: '#b36b2c', value: (point) => point.storage_write_iops ?? 0 }
          ]}
          valueLabel={formatIOPS}
        />
      )}
    </article>
  );
}

function summarizeProjectHistory(nodes: ProjectNodeMetricsHistory[]) {
  return nodes.reduce(
    (summary, node) => ({
      samples: summary.samples + node.summary.samples,
      maxCPU: Math.max(summary.maxCPU, node.summary.max_cpu_percent),
      maxRX: Math.max(summary.maxRX, node.summary.max_network_rx_bps),
      maxTX: Math.max(summary.maxTX, node.summary.max_network_tx_bps),
      maxRead: Math.max(summary.maxRead, node.summary.max_storage_read_bps),
      maxWrite: Math.max(summary.maxWrite, node.summary.max_storage_write_bps),
      maxReadIOPS: Math.max(summary.maxReadIOPS, node.summary.max_storage_read_iops ?? 0),
      maxWriteIOPS: Math.max(summary.maxWriteIOPS, node.summary.max_storage_write_iops ?? 0)
    }),
    { samples: 0, maxCPU: 0, maxRX: 0, maxTX: 0, maxRead: 0, maxWrite: 0, maxReadIOPS: 0, maxWriteIOPS: 0 }
  );
}

function projectCheckKey(result: ProjectCheckResult) {
  return `${result.service_id}-${result.timestamp}-${result.response_time_ms}-${result.detail}`;
}
