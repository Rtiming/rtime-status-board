import { useEffect, useState } from 'react';
import { Clock, Gauge, Layers3, ListChecks, TriangleAlert } from 'lucide-react';
import { fetchMetricReportLogs, fetchNodeChecks, fetchNodeDetail } from '../../api';
import { AgentReportRow, DetailResourceStatesPanel, Metric, MetricSummaryCard, Row, SubPanel } from '../../shared/components';
import { formatLatencyMS, formatSeconds, formatTime } from '../../shared/format';
import { dictionary, type Lang } from '../../shared/i18n';
import { metricRanges, type MetricRange } from '../../shared/ranges';
import { StatusDot, StatusPill, statusLabel } from '../../shared/status';
import type {
  MetricsReportLogsResponse,
  NodeCheckHistoryResponse,
  NodeCheckResult,
  NodeDetailResponse,
  NodeView
} from '../../types';

export function Nodes({
  nodes,
  lang,
  selectedNodeID,
  onSelectNode,
  onInspect
}: {
  nodes: NodeView[];
  lang: Lang;
  selectedNodeID: string;
  onSelectNode: (nodeID: string) => void;
  onInspect: (nodeID: string) => void;
}) {
  const t = dictionary[lang];
  const [detail, setDetail] = useState<NodeDetailResponse | null>(null);
  const [detailError, setDetailError] = useState('');
  const [detailLoading, setDetailLoading] = useState(false);
  const selectedNode = nodes.find((node) => node.id === selectedNodeID) ?? nodes[0];
  const selectedID = selectedNode?.id ?? selectedNodeID;

  useEffect(() => {
    if (nodes.length === 0) return;
    if (!selectedNodeID || !nodes.some((node) => node.id === selectedNodeID)) {
      onSelectNode(nodes[0].id);
    }
  }, [nodes, onSelectNode, selectedNodeID]);

  useEffect(() => {
    if (!selectedID) {
      setDetail(null);
      return;
    }
    let cancelled = false;
    const loadNode = async () => {
      setDetailLoading(true);
      try {
        const next = await fetchNodeDetail(selectedID);
        if (!cancelled) {
          setDetail(next);
          setDetailError('');
        }
      } catch (err) {
        if (!cancelled) {
          setDetailError(err instanceof Error ? err.message : t.nodeError);
        }
      } finally {
        if (!cancelled) {
          setDetailLoading(false);
        }
      }
    };
    void loadNode();
    return () => {
      cancelled = true;
    };
  }, [selectedID, t.nodeError]);

  return (
    <section className="stack">
      <section className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{t.node}</th>
              <th>{t.status}</th>
              <th>{t.tailnet}</th>
              <th>{t.role}</th>
              <th>{t.checks}</th>
              <th>{t.last}</th>
              <th>{t.detail}</th>
            </tr>
          </thead>
          <tbody>
            {nodes.map((node) => (
              <tr className={node.id === selectedID ? 'selected-row' : ''} key={node.id}>
                <td>
                  <button className="table-link" onClick={() => onSelectNode(node.id)} type="button">
                    <strong>{node.name}</strong>
                    <span>{node.hostname}</span>
                  </button>
                </td>
                <td><StatusPill status={node.status} lang={lang} /></td>
                <td>{node.tailnet_ip}</td>
                <td>{node.role}</td>
                <td>{node.service_count - node.down_count}/{node.service_count}</td>
                <td>{formatTime(node.last_checked_at)}</td>
                <td>
                  <button className="icon-button small-button" onClick={() => onInspect(node.id)} title={t.inspect} type="button">
                    <Gauge size={16} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      {detailError && (
        <div className="notice">
          <TriangleAlert size={18} />
          <span>{detailError}</span>
        </div>
      )}
      {detailLoading && <div className="loading-panel">{t.loadingNode}</div>}
      {selectedNode && detail && <NodeDetailPanel detail={detail} lang={lang} onInspect={onInspect} />}
    </section>
  );
}

function NodeDetailPanel({ detail, lang, onInspect }: { detail: NodeDetailResponse; lang: Lang; onInspect: (nodeID: string) => void }) {
  const t = dictionary[lang];
  const [checkRange, setCheckRange] = useState<MetricRange>('24h');
  const [checks, setChecks] = useState<NodeCheckHistoryResponse | null>(null);
  const [checksError, setChecksError] = useState('');
  const [checksLoading, setChecksLoading] = useState(false);
  const [agentReports, setAgentReports] = useState<MetricsReportLogsResponse | null>(null);
  const [agentReportsError, setAgentReportsError] = useState('');
  const [agentReportsLoading, setAgentReportsLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const loadNodeChecks = async () => {
      setChecksLoading(true);
      try {
        const next = await fetchNodeChecks(detail.node.id, checkRange, 60);
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
    void loadNodeChecks();
    return () => {
      cancelled = true;
    };
  }, [checkRange, detail.node.id, t.checksError]);

  useEffect(() => {
    let cancelled = false;
    const loadAgentReports = async () => {
      setAgentReportsLoading(true);
      try {
        const next = await fetchMetricReportLogs(detail.node.id, 12);
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
    void loadAgentReports();
    return () => {
      cancelled = true;
    };
  }, [detail.node.id, t.reportsError]);

  return (
    <section className="panel detail-panel">
      <div className="detail-head">
        <div>
          <div className="eyebrow">{t.nodeDetails}</div>
          <h2>{detail.node.name}</h2>
        </div>
        <div className="top-actions">
          <StatusPill status={detail.node.status} lang={lang} />
          <button className="icon-button small-button" onClick={() => onInspect(detail.node.id)} title={t.inspect} type="button">
            <Gauge size={16} />
          </button>
        </div>
      </div>

      <div className="detail-stat-grid">
        <Metric icon={ListChecks} label={t.healthy} value={`${detail.node.service_count - detail.node.down_count}/${detail.node.service_count}`} />
        <Metric icon={TriangleAlert} label={t.affected} value={String(detail.failures.length)} status={detail.failures.length > 0 ? 'degraded' : undefined} />
        <Metric icon={Layers3} label={t.relatedProjects} value={String(detail.projects.length)} />
        <Metric icon={Clock} label={t.last} value={formatTime(detail.node.last_checked_at)} />
      </div>

      <div className="project-detail-grid">
        <SubPanel title={t.relatedMetrics}>
          {!detail.metrics ? (
            <div className="empty inline-empty">{t.noMetrics}</div>
          ) : (
            <MetricSummaryCard metric={detail.metrics} lang={lang} />
          )}
        </SubPanel>

        <SubPanel title={t.failingChecks}>
          {detail.failures.length === 0 ? (
            <div className="empty inline-empty">{t.noFailures}</div>
          ) : (
            <div className="mini-list">
              {detail.failures.map((service) => (
                <Row key={service.id} title={service.name} subtitle={service.endpoint_key || service.kind} status={service.status} meta={service.detail} />
              ))}
            </div>
          )}
        </SubPanel>
      </div>

      <DetailResourceStatesPanel lang={lang} states={detail.resource_states ?? []} />

      <NodeCheckHistoryPanel
        checks={checks}
        checksError={checksError}
        checksLoading={checksLoading}
        lang={lang}
        range={checkRange}
        setRange={setCheckRange}
      />

      <NodeAgentReportsPanel
        lang={lang}
        reports={agentReports}
        reportsError={agentReportsError}
        reportsLoading={agentReportsLoading}
      />

      <div className="project-detail-grid">
        <SubPanel title={t.relatedProjects}>
          <div className="mini-list">
            {detail.projects.map((project) => (
              <Row key={project.id} title={project.name} subtitle={project.summary} status={project.status} meta={project.detail} />
            ))}
          </div>
        </SubPanel>

        <SubPanel title={t.relatedServices}>
          <div className="mini-list">
            {detail.services.map((service) => (
              <Row key={service.id} title={service.name} subtitle={service.project_id} status={service.status} meta={service.detail} />
            ))}
          </div>
        </SubPanel>
      </div>

      <SubPanel title={t.nodeEvents}>
        {detail.events.length === 0 ? (
          <div className="empty inline-empty">{t.noNodeEvents}</div>
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

function NodeCheckHistoryPanel({
  checks,
  checksError,
  checksLoading,
  lang,
  range,
  setRange
}: {
  checks: NodeCheckHistoryResponse | null;
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
    <SubPanel title={t.nodeCheckHistory}>
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
            <NodeCheckHistoryItem key={nodeCheckKey(result)} lang={lang} result={result} />
          ))}
        </div>
      )}
    </SubPanel>
  );
}

function NodeCheckHistoryItem({ lang, result }: { lang: Lang; result: NodeCheckResult }) {
  const t = dictionary[lang];
  return (
    <article className="check-history-item">
      <StatusDot status={result.status} />
      <div>
        <div className="check-history-head">
          <strong>{result.service_name}</strong>
          <span>{formatTime(result.timestamp)} · {result.response_time_ms > 0 ? `${result.response_time_ms}ms` : '-'}</span>
        </div>
        <p>{result.detail || statusLabel[lang][result.status]}</p>
        <span>{t.project}: {result.project_id || '-'}</span>
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

function NodeAgentReportsPanel({
  lang,
  reports,
  reportsError,
  reportsLoading
}: {
  lang: Lang;
  reports: MetricsReportLogsResponse | null;
  reportsError: string;
  reportsLoading: boolean;
}) {
  const t = dictionary[lang];
  const logs = reports?.logs ?? [];
  const latest = logs[0];
  const failedCount = logs.filter((report) => report.collector_failed > 0).length;
  return (
    <SubPanel title={t.recentAgentReports}>
      <div className="diag-kv compact-kv project-history-meta node-agent-report-meta">
        <span>{t.reports}</span>
        <strong>{reports?.returned ?? 0}</strong>
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
        <section className="table-wrap inline-table report-log-table node-report-log-table">
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

function nodeCheckKey(result: NodeCheckResult) {
  return `${result.service_id}-${result.timestamp}-${result.response_time_ms}-${result.detail}`;
}
