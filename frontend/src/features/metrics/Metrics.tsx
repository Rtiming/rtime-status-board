import { useEffect, useState } from 'react';
import { Activity, Gauge, Server, Signal, TriangleAlert } from 'lucide-react';
import { fetchNodeMetricsHistory } from '../../api';
import { HistoryChart, Metric, UsageBar } from '../../shared/components';
import { collectorIssueCount, firstGPUDevice, formatBytes, formatCount, formatIOPS, formatRate, formatSeconds, formatTime } from '../../shared/format';
import { dictionary, type Lang } from '../../shared/i18n';
import { initialMetricRange, metricRanges, type MetricRange } from '../../shared/ranges';
import { StatusDot } from '../../shared/status';
import type { MetricsHistoryResponse, MetricsView } from '../../types';

export function Metrics({
  metrics,
  lang,
  selectedNodeID,
  onSelectNode
}: {
  metrics: MetricsView[];
  lang: Lang;
  selectedNodeID: string;
  onSelectNode: (nodeID: string) => void;
}) {
  const t = dictionary[lang];
  const [range, setRange] = useState<MetricRange>(initialMetricRange);
  const [history, setHistory] = useState<MetricsHistoryResponse | null>(null);
  const [historyError, setHistoryError] = useState('');
  const [historyLoading, setHistoryLoading] = useState(false);
  const selectedMetric = metrics.find((metric) => metric.node_id === selectedNodeID) ?? metrics[0];
  const selectedID = selectedMetric?.node_id ?? selectedNodeID;

  useEffect(() => {
    if (metrics.length === 0) return;
    if (!selectedNodeID || !metrics.some((metric) => metric.node_id === selectedNodeID)) {
      onSelectNode(metrics[0].node_id);
    }
  }, [metrics, onSelectNode, selectedNodeID]);

  useEffect(() => {
    if (!selectedID) {
      setHistory(null);
      return;
    }
    let cancelled = false;
    const loadHistory = async () => {
      setHistoryLoading(true);
      try {
        const next = await fetchNodeMetricsHistory(selectedID, range);
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
    void loadHistory();
    return () => {
      cancelled = true;
    };
  }, [range, selectedID, t.historyError]);

  return (
    <section className="stack">
      {metrics.length === 0 && <div className="empty">{t.noMetrics}</div>}
      <div className="metric-node-grid">
        {metrics.map((metric) => (
          <button
            className={metric.node_id === selectedID ? 'metric-node active' : 'metric-node'}
            key={metric.node_id}
            onClick={() => onSelectNode(metric.node_id)}
            type="button"
          >
            <div className="card-head">
              <h2>{metric.node_id}</h2>
              {metric.stale && <span className="stale-label">{t.stale}</span>}
            </div>
            <div className="metric-bars">
              <UsageBar label={t.cpu} value={metric.cpu.percent} detail={`${metric.cpu.percent.toFixed(1)}%`} />
              <UsageBar label={t.memory} value={metric.memory.percent} detail={`${formatBytes(metric.memory.used_bytes)} / ${formatBytes(metric.memory.total_bytes)}`} />
              <UsageBar label={t.disk} value={metric.disk.percent} detail={`${formatBytes(metric.disk.used_bytes)} / ${formatBytes(metric.disk.total_bytes)}`} />
            </div>
            <div className="metric-detail-grid">
              <span>{t.storageIO}</span><strong>{t.read} {formatRate(metric.storage?.read_bps ?? 0)} · {t.write} {formatRate(metric.storage?.write_bps ?? 0)}</strong>
              <span>{t.storageOps}</span><strong>{t.read} {formatIOPS(metric.storage?.read_iops)} · {t.write} {formatIOPS(metric.storage?.write_iops)}</strong>
              <span>{t.network}</span><strong>{t.rx} {formatRate(metric.network.rx_bps)} · {t.tx} {formatRate(metric.network.tx_bps)}</strong>
              <span>{t.gpu}</span><strong>{firstGPUDevice(metric) ? `${firstGPUDevice(metric)?.util_percent.toFixed(1)}%` : t.noGpu}</strong>
              <span>{t.containers}</span><strong>{metric.containers?.available ? String(metric.containers?.containers?.length ?? 0) : t.unavailable}</strong>
              <span>{t.processes}</span><strong>{metric.processes?.process_count ? String(metric.processes.process_count) : '-'}</strong>
              <span>{t.collectors}</span><strong>{collectorIssueCount(metric) > 0 ? `${collectorIssueCount(metric)} ${t.collectorIssues}` : 'OK'}</strong>
              <span>{t.updated}</span><strong>{formatTime(metric.updated_at)}</strong>
            </div>
          </button>
        ))}
      </div>
      {selectedMetric && (
        <MetricHistoryPanel
          history={history}
          historyError={historyError}
          historyLoading={historyLoading}
          lang={lang}
          metric={selectedMetric}
          range={range}
          setRange={setRange}
        />
      )}
    </section>
  );
}

function MetricHistoryPanel({
  history,
  historyError,
  historyLoading,
  lang,
  metric,
  range,
  setRange
}: {
  history: MetricsHistoryResponse | null;
  historyError: string;
  historyLoading: boolean;
  lang: Lang;
  metric: MetricsView;
  range: MetricRange;
  setRange: (range: MetricRange) => void;
}) {
  const t = dictionary[lang];
  const points = history?.points ?? [];
  const latest = points.at(-1);
  const summary = history?.summary;
  return (
    <section className="panel detail-panel">
      <div className="detail-head">
        <div>
          <div className="eyebrow">{t.history}</div>
          <h2>{metric.node_id}</h2>
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
      {!historyLoading && points.length === 0 && <div className="empty inline-empty">{t.noHistory}</div>}

      <div className="detail-stat-grid">
        <Metric icon={Gauge} label={t.samples} value={String(summary?.samples ?? history?.returned ?? 0)} />
        <Metric icon={Activity} label={t.cpu} value={latest ? `${latest.cpu_percent.toFixed(1)}%` : `${metric.cpu.percent.toFixed(1)}%`} />
        <Metric icon={Signal} label={`${t.peak} ${t.network}`} value={summary ? `${formatRate(summary.max_network_rx_bps)} / ${formatRate(summary.max_network_tx_bps)}` : '-'} />
        <Metric icon={Server} label={`${t.peak} ${t.storageOps}`} value={summary ? `${formatIOPS(summary.max_storage_read_iops)} / ${formatIOPS(summary.max_storage_write_iops)}` : '-'} />
      </div>

      <MetricResourceDetails metric={metric} lang={lang} />

      {points.length > 1 && (
        <div className="chart-grid">
          <HistoryChart
            title={t.current}
            points={points}
            series={[
              { label: t.cpu, color: '#2f8f68', value: (point) => point.cpu_percent },
              { label: t.memory, color: '#3279c8', value: (point) => point.memory_percent },
              { label: t.disk, color: '#b36b2c', value: (point) => point.disk_percent },
              { label: t.gpu, color: '#7a4fb3', value: (point) => (point.gpu_available ? point.gpu_percent : 0) }
            ]}
            valueLabel={(value) => `${value.toFixed(0)}%`}
          />
          <HistoryChart
            title={t.network}
            points={points}
            series={[
              { label: t.rx, color: '#3279c8', value: (point) => point.network_rx_bps },
              { label: t.tx, color: '#2f8f68', value: (point) => point.network_tx_bps }
            ]}
            valueLabel={formatRate}
          />
          <HistoryChart
            title={t.storageIO}
            points={points}
            series={[
              { label: t.read, color: '#7a4fb3', value: (point) => point.storage_read_bps },
              { label: t.write, color: '#b36b2c', value: (point) => point.storage_write_bps }
            ]}
            valueLabel={formatRate}
          />
          <HistoryChart
            title={t.storageOps}
            points={points}
            series={[
              { label: t.read, color: '#7a4fb3', value: (point) => point.storage_read_iops ?? 0 },
              { label: t.write, color: '#b36b2c', value: (point) => point.storage_write_iops ?? 0 }
            ]}
            valueLabel={formatIOPS}
          />
        </div>
      )}
    </section>
  );
}

function MetricResourceDetails({ metric, lang }: { metric: MetricsView; lang: Lang }) {
  const t = dictionary[lang];
  const cores = metric.cpu.per_core_percent ?? [];
  const storageDevices = metric.storage?.devices ?? [];
  const networkInterfaces = metric.network.interfaces ?? [];
  const gpuDevices = metric.gpu?.devices ?? [];
  const containers = metric.containers?.containers ?? [];
  const processes = metric.processes?.processes ?? [];
  const collectors = metric.collector_status ?? [];

  return (
    <div className="resource-detail-grid">
      <article className="resource-card">
        <h2>{t.cpuDetail}</h2>
        <div className="diag-kv compact-kv">
          <span>{t.load}</span>
          <strong>{metric.cpu.load1.toFixed(2)} / {metric.cpu.load5.toFixed(2)} / {metric.cpu.load15.toFixed(2)}</strong>
          <span>{t.contextSwitches}</span>
          <strong>{formatCount(metric.cpu.context_switches ?? 0)}</strong>
          <span>{t.interrupts}</span>
          <strong>{formatCount(metric.cpu.interrupts ?? 0)}</strong>
        </div>
        {cores.length === 0 ? (
          <div className="empty inline-empty">{t.noPerCore}</div>
        ) : (
          <div className="core-grid" aria-label={t.perCore}>
            {cores.map((value, index) => (
              <span className="core-chip" key={`${index}-${value}`}>
                <span>C{index}</span>
                <strong>{value.toFixed(1)}%</strong>
              </span>
            ))}
          </div>
        )}
      </article>

      <article className="resource-card">
        <h2>{t.ioDetail}</h2>
        <div className="diag-kv compact-kv">
          <span>{t.read}</span>
          <strong>{formatRate(metric.storage?.read_bps ?? 0)}</strong>
          <span>{t.write}</span>
          <strong>{formatRate(metric.storage?.write_bps ?? 0)}</strong>
          <span>{t.iops}</span>
          <strong>{t.read} {formatIOPS(metric.storage?.read_iops)} · {t.write} {formatIOPS(metric.storage?.write_iops)}</strong>
          <span>{t.trafficTotal}</span>
          <strong>{t.read} {formatBytes(metric.storage?.read_bytes ?? 0)} · {t.write} {formatBytes(metric.storage?.write_bytes ?? 0)}</strong>
        </div>
        <DeviceList
          emptyText={`${t.devices}: -`}
          items={storageDevices.map((device) => ({
            key: device.name,
            title: device.name,
            lines: [
              `${t.read} ${formatBytes(device.read_bytes)} · ${t.write} ${formatBytes(device.write_bytes)}`,
              `IO ${formatCount(device.read_ios)} / ${formatCount(device.write_ios)}`
            ]
          }))}
        />
      </article>

      <article className="resource-card">
        <h2>{t.networkDetail}</h2>
        <div className="diag-kv compact-kv">
          <span>{t.rx}</span>
          <strong>{formatRate(metric.network.rx_bps)}</strong>
          <span>{t.tx}</span>
          <strong>{formatRate(metric.network.tx_bps)}</strong>
          <span>{t.trafficTotal}</span>
          <strong>{t.rx} {formatBytes(metric.network.rx_bytes)} · {t.tx} {formatBytes(metric.network.tx_bytes)}</strong>
        </div>
        <DeviceList
          emptyText={`${t.devices}: -`}
          items={networkInterfaces.map((iface) => ({
            key: iface.name,
            title: iface.name,
            lines: [
              `${t.rx} ${formatBytes(iface.rx_bytes)} · ${t.tx} ${formatBytes(iface.tx_bytes)}`,
              `${t.packets} ${formatCount(iface.rx_packets ?? 0)} / ${formatCount(iface.tx_packets ?? 0)}`,
              `${t.errorsDrops} ${(iface.rx_errors ?? 0) + (iface.tx_errors ?? 0)} / ${(iface.rx_drops ?? 0) + (iface.tx_drops ?? 0)}`
            ]
          }))}
        />
      </article>

      <article className="resource-card">
        <h2>{t.gpuDetail}</h2>
        <div className="diag-kv compact-kv">
          <span>{t.status}</span>
          <strong>{metric.gpu?.available ? 'OK' : t.noGpu}</strong>
          <span>{t.provider}</span>
          <strong>{metric.gpu?.provider || '-'}</strong>
        </div>
        <DeviceList
          emptyText={t.noGpu}
          items={gpuDevices.map((gpu) => ({
            key: `${gpu.index}-${gpu.name}`,
            title: gpu.name,
            lines: [
              `${t.current} ${gpu.util_percent.toFixed(1)}% · ${t.memory} ${formatBytes(gpu.memory_used_bytes ?? 0)} / ${formatBytes(gpu.memory_total_bytes ?? 0)}`,
              `${t.temperature} ${gpu.temperature_c ? `${gpu.temperature_c.toFixed(0)}C` : '-'} · ${t.power} ${gpu.power_watts ? `${gpu.power_watts.toFixed(1)}W` : '-'}`
            ]
          }))}
        />
      </article>

      <article className="resource-card">
        <h2>{t.containerDetail}</h2>
        <div className="diag-kv compact-kv">
          <span>{t.status}</span>
          <strong>{metric.containers?.available ? 'OK' : t.unavailable}</strong>
          <span>{t.provider}</span>
          <strong>{metric.containers?.provider || '-'}</strong>
          <span>{t.shown}</span>
          <strong>{String(containers.length)}</strong>
        </div>
        <DeviceList
          emptyText={metric.containers?.available ? `${t.containers}: -` : t.unavailable}
          items={containers.map((container) => ({
            key: `${container.id}-${container.name}`,
            title: container.name || container.id,
            lines: [
              `${t.cpu} ${container.cpu_percent.toFixed(1)}% · ${t.memory} ${formatBytes(container.memory_usage_bytes)} / ${formatBytes(container.memory_limit_bytes)} (${container.memory_percent.toFixed(1)}%)`,
              `${t.network} ${t.rx} ${formatBytes(container.network_rx_bytes)} · ${t.tx} ${formatBytes(container.network_tx_bytes)}`,
              `IO ${t.read} ${formatBytes(container.block_read_bytes)} · ${t.write} ${formatBytes(container.block_write_bytes)}`,
              `${t.image} ${container.image || '-'} · ${t.state} ${container.state || '-'}`,
              `${t.composeProject} ${container.compose_project || '-'}`
            ]
          }))}
        />
      </article>

      <article className="resource-card">
        <h2>{t.processDetail}</h2>
        <div className="diag-kv compact-kv">
          <span>{t.processCount}</span>
          <strong>{String(metric.processes?.process_count ?? 0)}</strong>
          <span>{t.shown}</span>
          <strong>{String(processes.length)}</strong>
        </div>
        <DeviceList
          emptyText={`${t.processes}: -`}
          items={processes.map((process) => ({
            key: `${process.pid}-${process.command}`,
            title: `${process.pid} · ${process.command}`,
            lines: [
              `PPID ${process.ppid} · ${t.user} ${process.user}`,
              `${t.cpu} ${process.cpu_percent.toFixed(1)}% · ${t.memory} ${process.memory_percent.toFixed(1)}% · ${t.rss} ${formatBytes(process.rss_bytes)}`,
              `${t.command} ${process.command}`
            ]
          }))}
        />
      </article>

      <article className="resource-card collector-card">
        <h2>{t.collectorDetail}</h2>
        {collectors.length === 0 ? (
          <div className="empty inline-empty">{t.collectors}: -</div>
        ) : (
          <div className="collector-list">
            {collectors.map((collector) => (
              <div className={collector.ok ? 'collector-item ok' : 'collector-item fail'} key={collector.name}>
                <StatusDot status={collector.ok ? 'ok' : 'down'} />
                <strong>{collector.name}</strong>
                <span>{collector.cached ? `${t.cached} ${formatSeconds(collector.cache_age_seconds ?? 0)}` : (collector.elapsed_ms ? `${collector.elapsed_ms}ms` : '-')}</span>
                <em>{collector.detail || (collector.ok ? 'OK' : '-')}</em>
              </div>
            ))}
          </div>
        )}
      </article>
    </div>
  );
}

function DeviceList({ emptyText, items }: { emptyText: string; items: Array<{ key: string; title: string; lines: string[] }> }) {
  if (items.length === 0) {
    return <div className="empty inline-empty">{emptyText}</div>;
  }
  return (
    <div className="device-list">
      {items.map((item) => (
        <div className="device-item" key={item.key}>
          <strong>{item.title}</strong>
          {item.lines.map((line) => (
            <span key={line}>{line}</span>
          ))}
        </div>
      ))}
    </div>
  );
}
