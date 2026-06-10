import type { DiagnosticsResponse, MetricsHistoryResponse, MetricsReportLogsResponse, NodeCheckHistoryResponse, NodeDetailResponse, ProjectCheckHistoryResponse, ProjectDetailResponse, ProjectMetricsHistoryResponse, ServiceCheckHistoryResponse, ServiceDetailResponse, SummaryResponse, TelemetrySchemaResponse } from './types';

export async function fetchSummary(): Promise<SummaryResponse> {
  const response = await fetch(new URL('api/v1/summary', window.location.href));
  if (!response.ok) {
    throw new Error(`API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchDiagnostics(): Promise<DiagnosticsResponse> {
  const response = await fetch(new URL('api/v1/diagnostics', window.location.href));
  if (!response.ok) {
    throw new Error(`Diagnostics API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchProjectDetail(projectId: string): Promise<ProjectDetailResponse> {
  const response = await fetch(new URL(`api/v1/projects/${encodeURIComponent(projectId)}`, window.location.href));
  if (!response.ok) {
    throw new Error(`Project detail API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchProjectMetricsHistory(projectId: string, range = '1h', limit = 3000): Promise<ProjectMetricsHistoryResponse> {
  const response = await fetch(new URL(`api/v1/projects/${encodeURIComponent(projectId)}/metrics?window=${encodeURIComponent(range)}&limit=${limit}`, window.location.href));
  if (!response.ok) {
    throw new Error(`Project metrics history API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchProjectChecks(projectId: string, range = '24h', limit = 60): Promise<ProjectCheckHistoryResponse> {
  const response = await fetch(new URL(`api/v1/projects/${encodeURIComponent(projectId)}/checks?window=${encodeURIComponent(range)}&limit=${limit}`, window.location.href));
  if (!response.ok) {
    throw new Error(`Project checks API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchNodeDetail(nodeId: string): Promise<NodeDetailResponse> {
  const response = await fetch(new URL(`api/v1/nodes/${encodeURIComponent(nodeId)}`, window.location.href));
  if (!response.ok) {
    throw new Error(`Node detail API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchNodeChecks(nodeId: string, range = '24h', limit = 60): Promise<NodeCheckHistoryResponse> {
  const response = await fetch(new URL(`api/v1/nodes/${encodeURIComponent(nodeId)}/checks?window=${encodeURIComponent(range)}&limit=${limit}`, window.location.href));
  if (!response.ok) {
    throw new Error(`Node checks API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchServiceDetail(serviceId: string): Promise<ServiceDetailResponse> {
  const response = await fetch(new URL(`api/v1/services/${encodeURIComponent(serviceId)}`, window.location.href));
  if (!response.ok) {
    throw new Error(`Service detail API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchServiceChecks(serviceId: string, range = '24h', limit = 30): Promise<ServiceCheckHistoryResponse> {
  const response = await fetch(new URL(`api/v1/services/${encodeURIComponent(serviceId)}/checks?window=${encodeURIComponent(range)}&limit=${limit}`, window.location.href));
  if (!response.ok) {
    throw new Error(`Service checks API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchNodeMetricsHistory(nodeId: string, range = '1h'): Promise<MetricsHistoryResponse> {
  const response = await fetch(new URL(`api/v1/nodes/${encodeURIComponent(nodeId)}/metrics?window=${encodeURIComponent(range)}`, window.location.href));
  if (!response.ok) {
    throw new Error(`Metrics history API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchMetricReportLogs(nodeId = '', limit = 30): Promise<MetricsReportLogsResponse> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (nodeId) {
    params.set('node_id', nodeId);
  }
  const response = await fetch(new URL(`api/v1/metrics/reports?${params.toString()}`, window.location.href));
  if (!response.ok) {
    throw new Error(`Metrics report logs API returned ${response.status}`);
  }
  return response.json();
}

export async function fetchTelemetrySchema(): Promise<TelemetrySchemaResponse> {
  const response = await fetch(new URL('api/v1/telemetry/schema', window.location.href));
  if (!response.ok) {
    throw new Error(`Telemetry schema API returned ${response.status}`);
  }
  return response.json();
}
