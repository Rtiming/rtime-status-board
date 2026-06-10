import type { DiagnosticsResponse, MetricsHistoryResponse, MetricsReportLogsResponse, NodeCheckHistoryResponse, NodeDetailResponse, ProjectCheckHistoryResponse, ProjectDetailResponse, ProjectMetricsHistoryResponse, ServiceCheckHistoryResponse, ServiceDetailResponse, SummaryResponse, TelemetrySchemaResponse } from './types';

async function apiErrorMessage(response: Response, label: string): Promise<string> {
  const base = `${label} returned ${response.status}`;
  const contentType = response.headers.get('content-type') ?? '';
  try {
    if (contentType.includes('application/json')) {
      const data = await response.json() as { error?: unknown; detail?: unknown; message?: unknown };
      const detail = data.error ?? data.detail ?? data.message;
      return typeof detail === 'string' && detail.trim() ? `${base}: ${detail}` : base;
    }
    const text = await response.text();
    return text.trim() ? `${base}: ${text.trim().slice(0, 240)}` : base;
  } catch {
    return base;
  }
}

async function requestJSON<T>(path: string, label: string): Promise<T> {
  const response = await fetch(new URL(path, window.location.href));
  if (!response.ok) {
    throw new Error(await apiErrorMessage(response, label));
  }
  return response.json() as Promise<T>;
}

export async function fetchSummary(): Promise<SummaryResponse> {
  return requestJSON<SummaryResponse>('api/v1/summary', 'Summary API');
}

export async function fetchDiagnostics(): Promise<DiagnosticsResponse> {
  return requestJSON<DiagnosticsResponse>('api/v1/diagnostics', 'Diagnostics API');
}

export async function fetchProjectDetail(projectId: string): Promise<ProjectDetailResponse> {
  return requestJSON<ProjectDetailResponse>(`api/v1/projects/${encodeURIComponent(projectId)}`, 'Project detail API');
}

export async function fetchProjectMetricsHistory(projectId: string, range = '1h', limit = 3000): Promise<ProjectMetricsHistoryResponse> {
  return requestJSON<ProjectMetricsHistoryResponse>(`api/v1/projects/${encodeURIComponent(projectId)}/metrics?window=${encodeURIComponent(range)}&limit=${limit}`, 'Project metrics history API');
}

export async function fetchProjectChecks(projectId: string, range = '24h', limit = 60): Promise<ProjectCheckHistoryResponse> {
  return requestJSON<ProjectCheckHistoryResponse>(`api/v1/projects/${encodeURIComponent(projectId)}/checks?window=${encodeURIComponent(range)}&limit=${limit}`, 'Project checks API');
}

export async function fetchNodeDetail(nodeId: string): Promise<NodeDetailResponse> {
  return requestJSON<NodeDetailResponse>(`api/v1/nodes/${encodeURIComponent(nodeId)}`, 'Node detail API');
}

export async function fetchNodeChecks(nodeId: string, range = '24h', limit = 60): Promise<NodeCheckHistoryResponse> {
  return requestJSON<NodeCheckHistoryResponse>(`api/v1/nodes/${encodeURIComponent(nodeId)}/checks?window=${encodeURIComponent(range)}&limit=${limit}`, 'Node checks API');
}

export async function fetchServiceDetail(serviceId: string): Promise<ServiceDetailResponse> {
  return requestJSON<ServiceDetailResponse>(`api/v1/services/${encodeURIComponent(serviceId)}`, 'Service detail API');
}

export async function fetchServiceChecks(serviceId: string, range = '24h', limit = 30): Promise<ServiceCheckHistoryResponse> {
  return requestJSON<ServiceCheckHistoryResponse>(`api/v1/services/${encodeURIComponent(serviceId)}/checks?window=${encodeURIComponent(range)}&limit=${limit}`, 'Service checks API');
}

export async function fetchNodeMetricsHistory(nodeId: string, range = '1h'): Promise<MetricsHistoryResponse> {
  return requestJSON<MetricsHistoryResponse>(`api/v1/nodes/${encodeURIComponent(nodeId)}/metrics?window=${encodeURIComponent(range)}`, 'Metrics history API');
}

export async function fetchMetricReportLogs(nodeId = '', limit = 30): Promise<MetricsReportLogsResponse> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (nodeId) {
    params.set('node_id', nodeId);
  }
  return requestJSON<MetricsReportLogsResponse>(`api/v1/metrics/reports?${params.toString()}`, 'Metrics report logs API');
}

export async function fetchTelemetrySchema(): Promise<TelemetrySchemaResponse> {
  return requestJSON<TelemetrySchemaResponse>('api/v1/telemetry/schema', 'Telemetry schema API');
}
