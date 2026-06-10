export type Status = 'ok' | 'degraded' | 'down' | 'unknown' | 'maintenance';

export interface AppMeta {
  name: string;
  description: string;
}

export interface StatusCounts {
  ok: number;
  degraded: number;
  down: number;
  unknown: number;
  maintenance: number;
}

export interface NodeView {
  id: string;
  name: string;
  hostname: string;
  tailnet_ip: string;
  location: string;
  role: string;
  tags: string[];
  status: Status;
  detail: string;
  service_count: number;
  down_count: number;
  last_checked_at?: string;
}

export interface ProjectView {
  id: string;
  name: string;
  summary: string;
  service_ids: string[];
  tags: string[];
  status: Status;
  detail: string;
  service_count: number;
  down_count: number;
  last_checked_at?: string;
}

export interface ServiceView {
  id: string;
  name: string;
  node_id: string;
  project_id: string;
  kind: string;
  target: string;
  endpoint_key: string;
  critical: boolean;
  description: string;
  tags: string[];
  status: Status;
  detail: string;
  response_time_ms: number;
  last_checked_at?: string;
}

export interface EventView {
  id: number;
  kind: string;
  subject_id: string;
  label: string;
  from?: Status;
  to: Status;
  detail: string;
  created_at: string;
}

export interface SummaryResponse {
  app: AppMeta;
  generated_at: string;
  overall: Status;
  counts: StatusCounts;
  nodes: NodeView[];
  projects: ProjectView[];
  services: ServiceView[];
  metrics: MetricsView[];
  events: EventView[];
}

export interface ProjectDetailResponse {
  generated_at: string;
  project: ProjectView;
  nodes: NodeView[];
  services: ServiceView[];
  metrics: MetricsView[];
  resource_states: OpsResourceState[];
  events: EventView[];
  failures: ServiceView[];
}

export interface NodeDetailResponse {
  generated_at: string;
  node: NodeView;
  services: ServiceView[];
  projects: ProjectView[];
  metrics?: MetricsView;
  resource_states: OpsResourceState[];
  events: EventView[];
  failures: ServiceView[];
}

export interface RuntimeCheckView {
  key: string;
  status: Status;
  success: boolean;
  detail: string;
  response_time_ms: number;
  last_checked_at?: string;
}

export interface ServiceDetailResponse {
  generated_at: string;
  service: ServiceView;
  node?: NodeView;
  project?: ProjectView;
  metrics?: MetricsView;
  latest_check?: RuntimeCheckView;
  events: EventView[];
}

export interface ServiceCheckHistoryResponse {
  generated_at: string;
  service: ServiceView;
  endpoint_key: string;
  window: string;
  results: ServiceCheckResult[];
  returned: number;
}

export interface ProjectCheckHistoryResponse {
  generated_at: string;
  project: ProjectView;
  window: string;
  endpoint_count: number;
  results: ProjectCheckResult[];
  returned: number;
}

export interface NodeCheckHistoryResponse {
  generated_at: string;
  node: NodeView;
  window: string;
  endpoint_count: number;
  results: NodeCheckResult[];
  returned: number;
}

export interface ServiceCheckResult {
  timestamp: string;
  status: Status;
  success: boolean;
  detail: string;
  response_time_ms: number;
  errors?: string[];
  conditions?: Array<{ condition: string; success: boolean }>;
}

export interface ProjectCheckResult {
  service_id: string;
  service_name: string;
  node_id: string;
  endpoint_key: string;
  timestamp: string;
  status: Status;
  success: boolean;
  detail: string;
  response_time_ms: number;
  errors?: string[];
  conditions?: Array<{ condition: string; success: boolean }>;
}

export interface NodeCheckResult {
  service_id: string;
  service_name: string;
  project_id: string;
  endpoint_key: string;
  timestamp: string;
  status: Status;
  success: boolean;
  detail: string;
  response_time_ms: number;
  errors?: string[];
  conditions?: Array<{ condition: string; success: boolean }>;
}

export interface DiagnosticsResponse {
  generated_at: string;
  overall: Status;
  counts: StatusCounts;
  providers: ProviderDiagnostic[];
  config: ConfigDiagnostic;
  metrics: MetricsDiagnostic;
  runtime?: RuntimeDiagnostic;
  deployment?: DeploymentDiagnostic;
  projects?: ProjectDiagnostic[];
  ops?: OpsDiagnostic;
  event_log?: EventLogDiagnostic;
  agent_health?: AgentNodeDiagnostic[];
  agent_reports: MetricsReportLog[];
  failures: ServiceView[];
  checks: RuntimeEndpointStatus[];
}

export interface EventLogDiagnostic {
  total: number;
  returned: number;
  latest_at?: string;
  kind_counts: EventKindCount[];
  status_counts: StatusCounts;
  events: EventView[];
}

export interface EventKindCount {
  kind: string;
  count: number;
}

export interface ProviderDiagnostic {
  name: string;
  status: Status;
  detail: string;
  latency_ms: number;
  checked_at: string;
}

export interface ConfigDiagnostic {
  node_count: number;
  project_count: number;
  service_count: number;
  gatus_endpoint_count: number;
  issues: ConfigIssue[];
}

export interface ConfigIssue {
  severity: string;
  kind: string;
  subject_id: string;
  detail: string;
}

export interface MetricsDiagnostic {
  expected_nodes: string[];
  reporting_nodes: string[];
  missing_nodes: string[];
  stale_nodes: string[];
  gpu_nodes: string[];
  collector_issues: MetricsCollectorIssue[];
  service_resource_budgets: ServiceResourceBudgetStatus[];
  service_resource_issues: ServiceResourceIssue[];
}

export interface MetricsCollectorIssue {
  node_id: string;
  name: string;
  detail?: string;
  elapsed_ms?: number;
}

export interface ServiceResourceBudgetStatus {
  service_id: string;
  service_name: string;
  node_id: string;
  status: Status;
  container_names: string[];
  matched_containers: string[];
  missing_containers?: string[];
  memory_usage_bytes: number;
  max_memory_bytes?: number;
  cpu_percent: number;
  max_cpu_percent?: number;
  detail: string;
}

export interface ServiceResourceIssue {
  service_id: string;
  service_name: string;
  node_id: string;
  severity: string;
  metric: string;
  value: number;
  limit?: number;
  unit?: string;
  container_name?: string;
  detail: string;
}

export interface RuntimeDiagnostic {
  uptime_seconds: number;
  go_version: string;
  goroutines: number;
  memory: RuntimeMemoryDiagnostic;
  summary_cache: SummaryCacheDiagnostic;
  store: StoreDiagnostic;
}

export interface RuntimeMemoryDiagnostic {
  alloc_bytes: number;
  sys_bytes: number;
  heap_alloc_bytes: number;
  heap_inuse_bytes: number;
  num_gc: number;
  last_gc_pause_ms: number;
}

export interface SummaryCacheDiagnostic {
  ttl_seconds: number;
  cached: boolean;
  expires_at?: string;
  seconds_until_expiry: number;
}

export interface StoreDiagnostic {
  path: string;
  db_size_bytes: number;
  wal_size_bytes: number;
  shm_size_bytes: number;
  total_size_bytes: number;
  status_cache_rows: number;
  event_rows: number;
  metrics_latest_rows: number;
  metrics_sample_rows: number;
  metrics_report_log_rows: number;
  latest_metric_at?: string;
  latest_report_at?: string;
  metrics_retention_days: number;
  report_log_retention_days: number;
  report_log_limit: number;
}

export interface DeploymentDiagnostic {
  status: Status;
  mode: string;
  detail: string;
  checks: DeploymentCheck[];
}

export interface DeploymentCheck {
  key: string;
  category: string;
  status: Status;
  expected?: string;
  actual?: string;
  detail: string;
}

export interface ProjectDiagnostic {
  project_id: string;
  project_name: string;
  status: Status;
  detail: string;
  service_count: number;
  critical_service_count: number;
  down_service_count: number;
  degraded_service_count: number;
  endpoint_count: number;
  missing_endpoint_count: number;
  unmapped_service_count: number;
  related_nodes: string[];
  metrics_reporting_nodes: string[];
  metrics_missing_nodes: string[];
  metrics_stale_nodes: string[];
  recent_check_count: number;
  recent_failure_count: number;
  last_check_at?: string;
  recent_event_count: number;
  last_event_at?: string;
}

export interface AgentNodeDiagnostic {
  node_id: string;
  hostname?: string;
  status: Status;
  detail: string;
  report_count: number;
  failed_report_count: number;
  collector_failure_count: number;
  latest_received_at?: string;
  latest_captured_at?: string;
  latest_report_lag_seconds: number;
  latest_schema_version: number;
  latest_collector_ok: number;
  latest_collector_failed: number;
  gpu_available: boolean;
  storage_device_count: number;
  network_interface_count: number;
  latest_failed_collectors?: AgentCollectorFailure[];
}

export interface AgentCollectorFailure {
  name: string;
  detail?: string;
  elapsed_ms?: number;
  cached?: boolean;
  cache_age_seconds?: number;
}

export interface OpsDiagnostic {
  issues: OpsIssue[];
  counts: OpsIssueCounts;
  resource_thresholds: EffectiveResourceThreshold[];
  resource_states: OpsResourceState[];
}

export interface OpsIssueCounts {
  error: number;
  warn: number;
  info: number;
}

export interface OpsIssue {
  severity: string;
  kind: string;
  subject_id: string;
  subject_name?: string;
  node_id?: string;
  project_id?: string;
  service_id?: string;
  status?: Status;
  metric?: string;
  value?: number;
  limit?: number;
  unit?: string;
  detail: string;
  observed_at?: string;
}

export interface EffectiveResourceThreshold {
  node_id: string;
  cpu_percent: number;
  memory_percent: number;
  root_disk_percent: number;
  gpu_util_percent: number;
  network_rx_bps?: number;
  network_tx_bps?: number;
  storage_read_bps?: number;
  storage_write_bps?: number;
}

export interface OpsResourceState {
  node_id: string;
  status: Status;
  detail: string;
  observed_at?: string;
  stale: boolean;
  cpu: ResourceHeadroom;
  memory: ResourceHeadroom;
  root_disk: ResourceHeadroom;
  gpu_available: boolean;
  gpu_name?: string;
  gpu: ResourceHeadroom;
  network_rx: ResourceHeadroom;
  network_tx: ResourceHeadroom;
  storage_read: ResourceHeadroom;
  storage_write: ResourceHeadroom;
}

export interface ResourceHeadroom {
  configured: boolean;
  value: number;
  limit?: number;
  headroom?: number;
  unit: string;
}

export interface MetricsReportLogsResponse {
  generated_at: string;
  node_id?: string;
  logs: MetricsReportLog[];
  returned: number;
}

export interface MetricsReportLog {
  id: number;
  node_id: string;
  hostname: string;
  schema_version: number;
  captured_at: string;
  received_at: string;
  report_lag_seconds: number;
  collector_ok: number;
  collector_failed: number;
  collector_status?: CollectorStatus[];
  gpu_available: boolean;
  storage_device_count: number;
  network_interface_count: number;
}

export interface RuntimeEndpointStatus {
  name: string;
  group: string;
  key: string;
  status: Status;
  success: boolean;
  detail: string;
  response_time_ms: number;
  last_checked_at?: string;
  recent_results: number;
  recent_failures: number;
  errors?: string[];
  conditions?: Array<{ condition: string; success: boolean }>;
}

export interface MetricsView {
  node_id: string;
  hostname: string;
  captured_at: string;
  updated_at: string;
  stale: boolean;
  schema_version: number;
  cpu: {
    percent: number;
    load1: number;
    load5: number;
    load15: number;
    per_core_percent?: number[];
    context_switches?: number;
    interrupts?: number;
  };
  memory: ResourceUsage;
  swap: ResourceUsage;
  disk: ResourceUsage & {
    mountpoint: string;
  };
  storage: {
    read_bytes: number;
    write_bytes: number;
    read_bps: number;
    write_bps: number;
    read_iops?: number;
    write_iops?: number;
    devices?: StorageDeviceMetric[];
  };
  network: {
    rx_bytes: number;
    tx_bytes: number;
    rx_bps: number;
    tx_bps: number;
    interfaces: NetworkInterfaceMetric[];
  };
  gpu: {
    available: boolean;
    provider: string;
    devices?: GPUDeviceMetric[];
  };
  containers: ContainerMetrics;
  processes: ProcessMetrics;
  uptime: {
    seconds: number;
  };
  collector_status?: CollectorStatus[];
}

export interface MetricsHistoryResponse {
  node_id: string;
  window: string;
  points: MetricsHistoryPoint[];
  returned: number;
}

export interface ProjectMetricsHistoryResponse {
  generated_at: string;
  project_id: string;
  window: string;
  nodes: ProjectNodeMetricsHistory[];
  returned: number;
}

export interface ProjectNodeMetricsHistory {
  node_id: string;
  summary: ProjectMetricsSummary;
  points: MetricsHistoryPoint[];
}

export interface ProjectMetricsSummary {
  samples: number;
  max_cpu_percent: number;
  max_memory_percent: number;
  max_disk_percent: number;
  max_network_rx_bps: number;
  max_network_tx_bps: number;
  max_storage_read_bps: number;
  max_storage_write_bps: number;
  max_storage_read_iops: number;
  max_storage_write_iops: number;
  gpu_available: boolean;
  max_gpu_percent: number;
}

export interface MetricsHistoryPoint {
  node_id: string;
  captured_at: string;
  cpu_percent: number;
  memory_percent: number;
  disk_percent: number;
  network_rx_bps: number;
  network_tx_bps: number;
  storage_read_bps: number;
  storage_write_bps: number;
  storage_read_iops: number;
  storage_write_iops: number;
  gpu_available: boolean;
  gpu_percent: number;
}

export interface TelemetrySchemaResponse {
  version: number;
  domains: Array<{
    id: string;
    label: string;
    optional: boolean;
    fields: string[];
  }>;
}

export interface ResourceUsage {
  total_bytes: number;
  used_bytes: number;
  percent: number;
}

export interface StorageDeviceMetric {
  name: string;
  read_bytes: number;
  write_bytes: number;
  read_ios: number;
  write_ios: number;
}

export interface NetworkInterfaceMetric {
  name: string;
  rx_bytes: number;
  tx_bytes: number;
  rx_packets?: number;
  tx_packets?: number;
  rx_errors?: number;
  tx_errors?: number;
  rx_drops?: number;
  tx_drops?: number;
}

export interface GPUDeviceMetric {
  index: string;
  name: string;
  util_percent: number;
  memory_total_bytes?: number;
  memory_used_bytes?: number;
  memory_percent?: number;
  temperature_c?: number;
  power_watts?: number;
}

export interface ContainerMetrics {
  available: boolean;
  provider: string;
  containers?: ContainerMetric[];
}

export interface ContainerMetric {
  id: string;
  name: string;
  image: string;
  state: string;
  compose_project?: string;
  cpu_percent: number;
  memory_percent: number;
  memory_usage_bytes: number;
  memory_limit_bytes: number;
  network_rx_bytes: number;
  network_tx_bytes: number;
  block_read_bytes: number;
  block_write_bytes: number;
}

export interface ProcessMetrics {
  process_count: number;
  processes?: ProcessMetric[];
}

export interface ProcessMetric {
  pid: number;
  ppid: number;
  user: string;
  command: string;
  cpu_percent: number;
  memory_percent: number;
  rss_bytes: number;
}

export interface CollectorStatus {
  name: string;
  ok: boolean;
  detail?: string;
  elapsed_ms?: number;
  cached?: boolean;
  cache_age_seconds?: number;
}
