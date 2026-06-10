import type { MetricsView, OpsIssue, ResourceHeadroom, Status, StatusCounts } from '../../types';
import type { Lang } from '../i18n';
import { statusLabel } from '../status';

export function formatTime(value?: string) {
  if (!value) return '-';
  return new Intl.DateTimeFormat(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  }).format(new Date(value));
}

export function formatEventKindCounts(counts: Array<{ kind: string; count: number }>) {
  if (counts.length === 0) return '-';
  return counts.map((item) => `${item.kind}: ${item.count}`).join(' / ');
}

export function formatStatusCountSummary(counts: StatusCounts, lang: Lang) {
  const items: Array<[Status, number]> = [
    ['down', counts.down],
    ['degraded', counts.degraded],
    ['unknown', counts.unknown],
    ['maintenance', counts.maintenance],
    ['ok', counts.ok]
  ];
  const active = items.filter(([, count]) => count > 0);
  if (active.length === 0) return '-';
  return active.map(([status, count]) => `${statusLabel[lang][status]} ${count}`).join(' / ');
}

export function formatOpsIssue(issue: OpsIssue) {
  const parts = [issue.detail];
  if (Number.isFinite(issue.value) && issue.unit) {
    const value = formatOpsValue(issue.value ?? 0, issue.unit);
    if (Number.isFinite(issue.limit) && issue.limit !== undefined && issue.limit > 0) {
      const limit = formatOpsValue(issue.limit, issue.unit);
      parts.push(`${value} / ${limit}`);
    } else {
      parts.push(value);
    }
  }
  if (issue.node_id) parts.push(issue.node_id);
  if (issue.observed_at) parts.push(formatTime(issue.observed_at));
  return parts.filter(Boolean).join(' · ');
}

export function formatOpsValue(value: number, unit: string) {
  if (unit === 'bytes') return formatBytes(value);
  if (unit === 'B/s') return formatRate(value);
  return `${value.toFixed(1)}${unit}`;
}

export function formatHeadroomCell(item: ResourceHeadroom | undefined) {
  if (!item || !item.configured || !Number.isFinite(item.limit)) return '-';
  const value = formatOpsValue(item.value ?? 0, item.unit);
  const limit = formatOpsValue(item.limit ?? 0, item.unit);
  const headroom = formatSignedOpsValue(item.headroom ?? 0, item.unit);
  return `${value} / ${limit} (${headroom})`;
}

export function formatList(items: string[] | undefined) {
  return items && items.length > 0 ? items.join(', ') : '-';
}

export function formatSignedOpsValue(value: number, unit: string) {
  const sign = value >= 0 ? '+' : '-';
  return `${sign}${formatOpsValue(Math.abs(value), unit)}`;
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '-';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let next = value;
  let index = 0;
  while (next >= 1024 && index < units.length - 1) {
    next /= 1024;
    index += 1;
  }
  return `${next.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

export function formatRate(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B/s';
  return `${formatBytes(value)}/s`;
}

export function formatIOPS(value: number | undefined) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return '0 ops/s';
  const digits = value >= 10 ? 0 : 1;
  return `${value.toFixed(digits)} ops/s`;
}

export function formatThresholdRate(value: number | undefined) {
  if (!Number.isFinite(value) || !value || value <= 0) return '-';
  return formatRate(value);
}

export function formatSeconds(value: number) {
  if (!Number.isFinite(value)) return '-';
  if (Math.abs(value) < 1) return `${Math.round(value * 1000)}ms`;
  return `${value.toFixed(1)}s`;
}

export function formatCount(value: number) {
  if (!Number.isFinite(value)) return '-';
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
}

export function firstGPUDevice(metric: MetricsView) {
  if (!metric.gpu?.available || !metric.gpu.devices?.length) return undefined;
  return metric.gpu.devices[0];
}

export function collectorIssueCount(metric: MetricsView) {
  return (metric.collector_status ?? []).filter((collector) => !collector.ok).length;
}

export function formatDuration(seconds: number, lang: Lang) {
  if (!Number.isFinite(seconds)) return '-';
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (lang === 'zh') {
    if (days > 0) return `${days}天 ${hours}小时`;
    if (hours > 0) return `${hours}小时 ${minutes}分`;
    return `${minutes}分`;
  }
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}
