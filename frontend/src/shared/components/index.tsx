import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import type { MetricsHistoryPoint, MetricsReportLog, MetricsView, OpsResourceState, Status } from '../../types';
import { collectorIssueCount, firstGPUDevice, formatBytes, formatHeadroomCell, formatIOPS, formatRate, formatSeconds, formatTime } from '../format';
import { dictionary, type Lang } from '../i18n';
import { StatusDot, StatusPill } from '../status';

export function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="panel">
      <h2>{title}</h2>
      {children}
    </section>
  );
}

export function SubPanel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="subpanel">
      <h2>{title}</h2>
      {children}
    </section>
  );
}

export function Row({ title, subtitle, status, meta }: { title: string; subtitle: string; status: Status; meta: string }) {
  return (
    <div className="mini-row">
      <StatusDot status={status} />
      <div>
        <strong>{title}</strong>
        <span>{subtitle}</span>
      </div>
      <em>{meta}</em>
    </div>
  );
}

export function Metric({ icon: Icon, label, value, status }: { icon: LucideIcon; label: string; value: string; status?: Status }) {
  return (
    <article className={status ? `metric status-${status}` : 'metric'}>
      <Icon size={20} />
      <span>{label}</span>
      <strong>{value}</strong>
    </article>
  );
}

export function UsageBar({ label, value, detail }: { label: string; value: number; detail: string }) {
  return (
    <div className="usage">
      <div className="usage-head">
        <span>{label}</span>
        <strong>{detail}</strong>
      </div>
      <div className="usage-track">
        <div className="usage-fill" style={{ width: `${Math.min(Math.max(value, 0), 100)}%` }} />
      </div>
    </div>
  );
}

export function AgentReportRow({ report, lang }: { report: MetricsReportLog; lang: Lang }) {
  const t = dictionary[lang];
  const failedCollectors = (report.collector_status ?? []).filter((collector) => !collector.ok);
  const status: Status = report.collector_failed > 0 ? 'degraded' : 'ok';
  return (
    <tr>
      <td>
        <strong>{report.node_id}</strong>
        <span>{report.hostname}</span>
      </td>
      <td>
        <strong>{formatTime(report.received_at)}</strong>
        <span>{t.captured}: {formatTime(report.captured_at)}</span>
      </td>
      <td><StatusPill status={status} lang={lang} /></td>
      <td>v{report.schema_version}</td>
      <td>{formatSeconds(report.report_lag_seconds)}</td>
      <td>
        <strong>{report.collector_ok}/{report.collector_failed}</strong>
        <span>
          {t.storageDevices}: {report.storage_device_count} · {t.interfaces}: {report.network_interface_count} · {t.gpu}: {report.gpu_available ? 'OK' : '-'}
        </span>
        {failedCollectors.length > 0 && <span>{failedCollectors.map((collector) => `${collector.name}: ${collector.detail || '-'}`).join('; ')}</span>}
      </td>
    </tr>
  );
}

export function DetailResourceStatesPanel({ lang, states }: { lang: Lang; states: OpsResourceState[] }) {
  const t = dictionary[lang];
  return (
    <SubPanel title={t.resourceExplanation}>
      {states.length === 0 ? (
        <div className="empty inline-empty">{t.noMetrics}</div>
      ) : (
        <div className="resource-state-grid detail-resource-state-grid">
          {states.map((state) => (
            <article className="resource-card resource-state-card" key={state.node_id}>
              <div className="card-head">
                <h2>{state.node_id}</h2>
                <StatusPill status={state.status} lang={lang} />
              </div>
              <p>{state.detail}</p>
              <div className="diag-kv compact-kv">
                <span>{t.cpu}</span>
                <strong>{formatHeadroomCell(state.cpu)}</strong>
                <span>{t.memory}</span>
                <strong>{formatHeadroomCell(state.memory)}</strong>
                <span>{t.disk}</span>
                <strong>{formatHeadroomCell(state.root_disk)}</strong>
                <span>{t.gpu}</span>
                <strong>{state.gpu_available ? formatHeadroomCell(state.gpu) : t.noGpu}</strong>
                <span>{t.rx}</span>
                <strong>{formatHeadroomCell(state.network_rx)}</strong>
                <span>{t.tx}</span>
                <strong>{formatHeadroomCell(state.network_tx)}</strong>
                <span>{t.read}</span>
                <strong>{formatHeadroomCell(state.storage_read)}</strong>
                <span>{t.write}</span>
                <strong>{formatHeadroomCell(state.storage_write)}</strong>
                <span>{t.updated}</span>
                <strong>{formatTime(state.observed_at)}</strong>
              </div>
            </article>
          ))}
        </div>
      )}
    </SubPanel>
  );
}

export function MetricSummaryCard({ metric, lang }: { metric: MetricsView; lang: Lang }) {
  const t = dictionary[lang];
  const gpu = firstGPUDevice(metric);
  const issueCount = collectorIssueCount(metric);
  const containerCount = metric.containers?.containers?.length ?? 0;
  const processCount = metric.processes?.process_count ?? 0;
  return (
    <article className={metric.stale ? 'provider-card stale-card' : 'provider-card'}>
      <div className="card-head">
        <h2>{metric.node_id}</h2>
        {metric.stale && <span className="stale-label">{t.stale}</span>}
      </div>
      <div className="diag-kv compact-kv">
        <span>{t.cpu}</span>
        <strong>{metric.cpu.percent.toFixed(1)}%{metric.cpu.per_core_percent?.length ? ` · ${metric.cpu.per_core_percent.length} ${t.perCore}` : ''}</strong>
        <span>{t.memory}</span>
        <strong>{formatBytes(metric.memory.used_bytes)} / {formatBytes(metric.memory.total_bytes)}</strong>
        <span>{t.disk}</span>
        <strong>{metric.disk.percent.toFixed(1)}%</strong>
        <span>{t.storageIO}</span>
        <strong>{t.read} {formatRate(metric.storage?.read_bps ?? 0)} · {t.write} {formatRate(metric.storage?.write_bps ?? 0)}</strong>
        <span>{t.storageOps}</span>
        <strong>{t.read} {formatIOPS(metric.storage?.read_iops)} · {t.write} {formatIOPS(metric.storage?.write_iops)}</strong>
        <span>{t.network}</span>
        <strong>{t.rx} {formatRate(metric.network.rx_bps)} · {t.tx} {formatRate(metric.network.tx_bps)}</strong>
        <span>{t.gpu}</span>
        <strong>{gpu ? `${gpu.name} · ${gpu.util_percent.toFixed(1)}%` : t.noGpu}</strong>
        <span>{t.containers}</span>
        <strong>{metric.containers?.available ? String(containerCount) : t.unavailable}</strong>
        <span>{t.processes}</span>
        <strong>{processCount > 0 ? String(processCount) : '-'}</strong>
        <span>{t.collectors}</span>
        <strong>{issueCount > 0 ? `${issueCount} ${t.collectorIssues}` : 'OK'}</strong>
      </div>
    </article>
  );
}

export function HistoryChart({
  points,
  series,
  title,
  valueLabel
}: {
  points: MetricsHistoryPoint[];
  series: Array<{ label: string; color: string; value: (point: MetricsHistoryPoint) => number }>;
  title: string;
  valueLabel: (value: number) => string;
}) {
  const width = 640;
  const height = 180;
  const padX = 28;
  const padY = 22;
  const values = points.flatMap((point) => series.map((item) => item.value(point))).filter(Number.isFinite);
  const maxValue = Math.max(1, ...values);
  const minValue = Math.min(0, ...values);
  const span = Math.max(maxValue - minValue, 1);
  const xFor = (index: number) => padX + (index / Math.max(points.length - 1, 1)) * (width - padX * 2);
  const yFor = (value: number) => height - padY - ((value - minValue) / span) * (height - padY * 2);

  return (
    <article className="chart-panel">
      <div className="chart-head">
        <h2>{title}</h2>
        <span>{valueLabel(maxValue)}</span>
      </div>
      <svg className="line-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label={title}>
        <line className="chart-grid-line" x1={padX} x2={width - padX} y1={height - padY} y2={height - padY} />
        <line className="chart-grid-line" x1={padX} x2={width - padX} y1={padY} y2={padY} />
        {series.map((item) => {
          const path = points
            .map((point, index) => `${index === 0 ? 'M' : 'L'} ${xFor(index).toFixed(2)} ${yFor(item.value(point)).toFixed(2)}`)
            .join(' ');
          return <path className="chart-line" d={path} key={item.label} stroke={item.color} />;
        })}
      </svg>
      <div className="chart-legend">
        {series.map((item) => (
          <span key={item.label}>
            <i style={{ background: item.color }} />
            {item.label}
          </span>
        ))}
      </div>
    </article>
  );
}
