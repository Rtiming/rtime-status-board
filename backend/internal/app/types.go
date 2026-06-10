package app

import "time"

type Status string

const (
	StatusOK          Status = "ok"
	StatusDegraded    Status = "degraded"
	StatusDown        Status = "down"
	StatusUnknown     Status = "unknown"
	StatusMaintenance Status = "maintenance"
)

type AppConfig struct {
	App         AppMeta           `yaml:"app" json:"app"`
	Diagnostics DiagnosticsConfig `yaml:"diagnostics" json:"diagnostics"`
	Nodes       []NodeConfig      `yaml:"nodes" json:"nodes"`
	Projects    []ProjectConfig   `yaml:"projects" json:"projects"`
	Services    []ServiceConfig   `yaml:"services" json:"services"`
}

type AppMeta struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

type DiagnosticsConfig struct {
	ResourceThresholds ResourceThresholdConfig `yaml:"resource_thresholds" json:"resource_thresholds"`
}

type ResourceThresholdConfig struct {
	CPUPercent      *float64                             `yaml:"cpu_percent,omitempty" json:"cpu_percent,omitempty"`
	MemoryPercent   *float64                             `yaml:"memory_percent,omitempty" json:"memory_percent,omitempty"`
	RootDiskPercent *float64                             `yaml:"root_disk_percent,omitempty" json:"root_disk_percent,omitempty"`
	GPUUtilPercent  *float64                             `yaml:"gpu_util_percent,omitempty" json:"gpu_util_percent,omitempty"`
	NetworkRXBps    *float64                             `yaml:"network_rx_bps,omitempty" json:"network_rx_bps,omitempty"`
	NetworkTXBps    *float64                             `yaml:"network_tx_bps,omitempty" json:"network_tx_bps,omitempty"`
	StorageReadBps  *float64                             `yaml:"storage_read_bps,omitempty" json:"storage_read_bps,omitempty"`
	StorageWriteBps *float64                             `yaml:"storage_write_bps,omitempty" json:"storage_write_bps,omitempty"`
	Nodes           map[string]ResourceThresholdOverride `yaml:"nodes,omitempty" json:"nodes,omitempty"`
}

type ResourceThresholdOverride struct {
	CPUPercent      *float64 `yaml:"cpu_percent,omitempty" json:"cpu_percent,omitempty"`
	MemoryPercent   *float64 `yaml:"memory_percent,omitempty" json:"memory_percent,omitempty"`
	RootDiskPercent *float64 `yaml:"root_disk_percent,omitempty" json:"root_disk_percent,omitempty"`
	GPUUtilPercent  *float64 `yaml:"gpu_util_percent,omitempty" json:"gpu_util_percent,omitempty"`
	NetworkRXBps    *float64 `yaml:"network_rx_bps,omitempty" json:"network_rx_bps,omitempty"`
	NetworkTXBps    *float64 `yaml:"network_tx_bps,omitempty" json:"network_tx_bps,omitempty"`
	StorageReadBps  *float64 `yaml:"storage_read_bps,omitempty" json:"storage_read_bps,omitempty"`
	StorageWriteBps *float64 `yaml:"storage_write_bps,omitempty" json:"storage_write_bps,omitempty"`
}

type NodeConfig struct {
	ID        string   `yaml:"id" json:"id"`
	Name      string   `yaml:"name" json:"name"`
	Hostname  string   `yaml:"hostname" json:"hostname"`
	TailnetIP string   `yaml:"tailnet_ip" json:"tailnet_ip"`
	Location  string   `yaml:"location" json:"location"`
	Role      string   `yaml:"role" json:"role"`
	Tags      []string `yaml:"tags" json:"tags"`
}

type ProjectConfig struct {
	ID         string   `yaml:"id" json:"id"`
	Name       string   `yaml:"name" json:"name"`
	Summary    string   `yaml:"summary" json:"summary"`
	ServiceIDs []string `yaml:"service_ids" json:"service_ids"`
	Tags       []string `yaml:"tags" json:"tags"`
}

type ServiceConfig struct {
	ID             string                 `yaml:"id" json:"id"`
	Name           string                 `yaml:"name" json:"name"`
	NodeID         string                 `yaml:"node_id" json:"node_id"`
	ProjectID      string                 `yaml:"project_id" json:"project_id"`
	Kind           string                 `yaml:"kind" json:"kind"`
	Target         string                 `yaml:"target" json:"target"`
	EndpointKey    string                 `yaml:"endpoint_key" json:"endpoint_key"`
	Critical       bool                   `yaml:"critical" json:"critical"`
	Description    string                 `yaml:"description" json:"description"`
	ResourceBudget *ServiceResourceBudget `yaml:"resource_budget,omitempty" json:"resource_budget,omitempty"`
	Tags           []string               `yaml:"tags" json:"tags"`
}

type ServiceResourceBudget struct {
	ContainerNames []string `yaml:"container_names" json:"container_names,omitempty"`
	ComposeProject string   `yaml:"compose_project" json:"compose_project,omitempty"`
	MaxMemoryMiB   float64  `yaml:"max_memory_mib" json:"max_memory_mib,omitempty"`
	MaxCPUPercent  float64  `yaml:"max_cpu_percent" json:"max_cpu_percent,omitempty"`
}

type RuntimeCheck struct {
	Key            string    `json:"key"`
	Status         Status    `json:"status"`
	Success        bool      `json:"success"`
	Detail         string    `json:"detail"`
	ResponseTimeMS int64     `json:"response_time_ms"`
	LastCheckedAt  time.Time `json:"last_checked_at,omitempty"`
}

type RuntimeEndpointStatus struct {
	Name           string                    `json:"name"`
	Group          string                    `json:"group"`
	Key            string                    `json:"key"`
	Status         Status                    `json:"status"`
	Success        bool                      `json:"success"`
	Detail         string                    `json:"detail"`
	ResponseTimeMS int64                     `json:"response_time_ms"`
	LastCheckedAt  time.Time                 `json:"last_checked_at,omitempty"`
	RecentResults  int                       `json:"recent_results"`
	RecentFailures int                       `json:"recent_failures"`
	Errors         []string                  `json:"errors,omitempty"`
	Conditions     []EndpointConditionResult `json:"conditions,omitempty"`
}

type EndpointConditionResult struct {
	Condition string `json:"condition"`
	Success   bool   `json:"success"`
}

type NodeView struct {
	NodeConfig
	Status        Status    `json:"status"`
	Detail        string    `json:"detail"`
	ServiceCount  int       `json:"service_count"`
	DownCount     int       `json:"down_count"`
	LastCheckedAt time.Time `json:"last_checked_at,omitempty"`
}

type ServiceView struct {
	ServiceConfig
	Status         Status    `json:"status"`
	Detail         string    `json:"detail"`
	ResponseTimeMS int64     `json:"response_time_ms"`
	LastCheckedAt  time.Time `json:"last_checked_at,omitempty"`
}

type ProjectView struct {
	ProjectConfig
	Status        Status    `json:"status"`
	Detail        string    `json:"detail"`
	ServiceCount  int       `json:"service_count"`
	DownCount     int       `json:"down_count"`
	LastCheckedAt time.Time `json:"last_checked_at,omitempty"`
}

type SummaryResponse struct {
	App         AppMeta       `json:"app"`
	GeneratedAt time.Time     `json:"generated_at"`
	Overall     Status        `json:"overall"`
	Counts      StatusCounts  `json:"counts"`
	Nodes       []NodeView    `json:"nodes"`
	Projects    []ProjectView `json:"projects"`
	Services    []ServiceView `json:"services"`
	Metrics     []MetricsView `json:"metrics"`
	Events      []Event       `json:"events"`
}

type ProjectDetailResponse struct {
	GeneratedAt    time.Time          `json:"generated_at"`
	Project        ProjectView        `json:"project"`
	Nodes          []NodeView         `json:"nodes"`
	Services       []ServiceView      `json:"services"`
	Metrics        []MetricsView      `json:"metrics"`
	ResourceStates []OpsResourceState `json:"resource_states"`
	Events         []Event            `json:"events"`
	Failures       []ServiceView      `json:"failures"`
}

type NodeDetailResponse struct {
	GeneratedAt    time.Time          `json:"generated_at"`
	Node           NodeView           `json:"node"`
	Services       []ServiceView      `json:"services"`
	Projects       []ProjectView      `json:"projects"`
	Metrics        *MetricsView       `json:"metrics,omitempty"`
	ResourceStates []OpsResourceState `json:"resource_states"`
	Events         []Event            `json:"events"`
	Failures       []ServiceView      `json:"failures"`
}

type ServiceDetailResponse struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Service     ServiceView   `json:"service"`
	Node        *NodeView     `json:"node,omitempty"`
	Project     *ProjectView  `json:"project,omitempty"`
	Metrics     *MetricsView  `json:"metrics,omitempty"`
	LatestCheck *RuntimeCheck `json:"latest_check,omitempty"`
	Events      []Event       `json:"events"`
}

type ServiceCheckHistoryResponse struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Service     ServiceView          `json:"service"`
	EndpointKey string               `json:"endpoint_key"`
	Window      string               `json:"window"`
	Summary     CheckHistorySummary  `json:"summary"`
	Results     []ServiceCheckResult `json:"results"`
	Returned    int                  `json:"returned"`
}

type ProjectCheckHistoryResponse struct {
	GeneratedAt   time.Time            `json:"generated_at"`
	Project       ProjectView          `json:"project"`
	Window        string               `json:"window"`
	EndpointCount int                  `json:"endpoint_count"`
	Summary       CheckHistorySummary  `json:"summary"`
	Results       []ProjectCheckResult `json:"results"`
	Returned      int                  `json:"returned"`
}

type NodeCheckHistoryResponse struct {
	GeneratedAt   time.Time           `json:"generated_at"`
	Node          NodeView            `json:"node"`
	Window        string              `json:"window"`
	EndpointCount int                 `json:"endpoint_count"`
	Summary       CheckHistorySummary `json:"summary"`
	Results       []NodeCheckResult   `json:"results"`
	Returned      int                 `json:"returned"`
}

type CheckHistorySummary struct {
	Total             int        `json:"total"`
	Successes         int        `json:"successes"`
	Failures          int        `json:"failures"`
	FailurePercent    float64    `json:"failure_percent"`
	AvgResponseTimeMS float64    `json:"avg_response_time_ms"`
	P95ResponseTimeMS int64      `json:"p95_response_time_ms"`
	MaxResponseTimeMS int64      `json:"max_response_time_ms"`
	LastFailureAt     *time.Time `json:"last_failure_at,omitempty"`
}

type ServiceCheckResult struct {
	Timestamp      time.Time                 `json:"timestamp"`
	Status         Status                    `json:"status"`
	Success        bool                      `json:"success"`
	Detail         string                    `json:"detail"`
	ResponseTimeMS int64                     `json:"response_time_ms"`
	Errors         []string                  `json:"errors,omitempty"`
	Conditions     []EndpointConditionResult `json:"conditions,omitempty"`
}

type NodeCheckResult struct {
	ServiceID      string                    `json:"service_id"`
	ServiceName    string                    `json:"service_name"`
	ProjectID      string                    `json:"project_id"`
	EndpointKey    string                    `json:"endpoint_key"`
	Timestamp      time.Time                 `json:"timestamp"`
	Status         Status                    `json:"status"`
	Success        bool                      `json:"success"`
	Detail         string                    `json:"detail"`
	ResponseTimeMS int64                     `json:"response_time_ms"`
	Errors         []string                  `json:"errors,omitempty"`
	Conditions     []EndpointConditionResult `json:"conditions,omitempty"`
}

type ProjectCheckResult struct {
	ServiceID      string                    `json:"service_id"`
	ServiceName    string                    `json:"service_name"`
	NodeID         string                    `json:"node_id"`
	EndpointKey    string                    `json:"endpoint_key"`
	Timestamp      time.Time                 `json:"timestamp"`
	Status         Status                    `json:"status"`
	Success        bool                      `json:"success"`
	Detail         string                    `json:"detail"`
	ResponseTimeMS int64                     `json:"response_time_ms"`
	Errors         []string                  `json:"errors,omitempty"`
	Conditions     []EndpointConditionResult `json:"conditions,omitempty"`
}

type DiagnosticsResponse struct {
	GeneratedAt  time.Time               `json:"generated_at"`
	Overall      Status                  `json:"overall"`
	Counts       StatusCounts            `json:"counts"`
	Providers    []ProviderDiagnostic    `json:"providers"`
	Config       ConfigDiagnostic        `json:"config"`
	Metrics      MetricsDiagnostic       `json:"metrics"`
	Runtime      RuntimeDiagnostic       `json:"runtime"`
	Deployment   DeploymentDiagnostic    `json:"deployment"`
	Projects     []ProjectDiagnostic     `json:"projects"`
	Ops          OpsDiagnostic           `json:"ops"`
	EventLog     EventLogDiagnostic      `json:"event_log"`
	AgentHealth  []AgentNodeDiagnostic   `json:"agent_health"`
	AgentReports []MetricsReportLog      `json:"agent_reports"`
	Failures     []ServiceView           `json:"failures"`
	Checks       []RuntimeEndpointStatus `json:"checks"`
}

type ProviderDiagnostic struct {
	Name      string    `json:"name"`
	Status    Status    `json:"status"`
	Detail    string    `json:"detail"`
	LatencyMS int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
}

type ConfigDiagnostic struct {
	NodeCount          int           `json:"node_count"`
	ProjectCount       int           `json:"project_count"`
	ServiceCount       int           `json:"service_count"`
	GatusEndpointCount int           `json:"gatus_endpoint_count"`
	Issues             []ConfigIssue `json:"issues"`
}

type ConfigIssue struct {
	Severity  string `json:"severity"`
	Kind      string `json:"kind"`
	SubjectID string `json:"subject_id"`
	Detail    string `json:"detail"`
}

type MetricsDiagnostic struct {
	ExpectedNodes          []string                      `json:"expected_nodes"`
	ReportingNodes         []string                      `json:"reporting_nodes"`
	MissingNodes           []string                      `json:"missing_nodes"`
	StaleNodes             []string                      `json:"stale_nodes"`
	GPUNodes               []string                      `json:"gpu_nodes"`
	CollectorIssues        []MetricsCollectorIssue       `json:"collector_issues"`
	CollectorSummary       []MetricsCollectorSummary     `json:"collector_summary"`
	ServiceResourceBudgets []ServiceResourceBudgetStatus `json:"service_resource_budgets"`
	ServiceResourceIssues  []ServiceResourceIssue        `json:"service_resource_issues"`
}

type MetricsCollectorIssue struct {
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	Detail    string `json:"detail,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms,omitempty"`
}

type MetricsCollectorSummary struct {
	Name               string   `json:"name"`
	Status             Status   `json:"status"`
	ReportingNodes     int      `json:"reporting_nodes"`
	ObservedNodes      int      `json:"observed_nodes"`
	OKNodes            int      `json:"ok_nodes"`
	FailedNodes        int      `json:"failed_nodes"`
	CachedNodes        int      `json:"cached_nodes"`
	StaleCachedNodes   int      `json:"stale_cached_nodes"`
	MissingNodes       []string `json:"missing_nodes,omitempty"`
	FailedNodeIDs      []string `json:"failed_node_ids,omitempty"`
	CachedNodeIDs      []string `json:"cached_node_ids,omitempty"`
	StaleCachedNodeIDs []string `json:"stale_cached_node_ids,omitempty"`
	AvgElapsedMS       float64  `json:"avg_elapsed_ms"`
	MaxElapsedMS       int64    `json:"max_elapsed_ms"`
	MaxCacheAgeSeconds float64  `json:"max_cache_age_seconds"`
	CacheWarnSeconds   float64  `json:"cache_warn_seconds,omitempty"`
	Detail             string   `json:"detail"`
}

type ServiceResourceBudgetStatus struct {
	ServiceID           string   `json:"service_id"`
	ServiceName         string   `json:"service_name"`
	NodeID              string   `json:"node_id"`
	Status              Status   `json:"status"`
	ContainerNames      []string `json:"container_names"`
	MatchedContainers   []string `json:"matched_containers"`
	MissingContainers   []string `json:"missing_containers,omitempty"`
	MemoryUsageBytes    int64    `json:"memory_usage_bytes"`
	MaxMemoryBytes      int64    `json:"max_memory_bytes,omitempty"`
	MemoryUsagePercent  float64  `json:"memory_usage_percent"`
	MemoryHeadroomBytes int64    `json:"memory_headroom_bytes"`
	CPUPercent          float64  `json:"cpu_percent"`
	MaxCPUPercent       float64  `json:"max_cpu_percent,omitempty"`
	CPUHeadroomPercent  float64  `json:"cpu_headroom_percent"`
	Detail              string   `json:"detail"`
}

type ServiceResourceIssue struct {
	ServiceID     string  `json:"service_id"`
	ServiceName   string  `json:"service_name"`
	NodeID        string  `json:"node_id"`
	Severity      string  `json:"severity"`
	Metric        string  `json:"metric"`
	Value         float64 `json:"value"`
	Limit         float64 `json:"limit,omitempty"`
	Unit          string  `json:"unit,omitempty"`
	ContainerName string  `json:"container_name,omitempty"`
	Detail        string  `json:"detail"`
}

type RuntimeDiagnostic struct {
	UptimeSeconds float64                 `json:"uptime_seconds"`
	GoVersion     string                  `json:"go_version"`
	Goroutines    int                     `json:"goroutines"`
	Build         RuntimeBuildDiagnostic  `json:"build"`
	Diagnostics   RuntimeTimingDiagnostic `json:"diagnostics"`
	Memory        RuntimeMemoryDiagnostic `json:"memory"`
	SummaryCache  SummaryCacheDiagnostic  `json:"summary_cache"`
	Store         StoreDiagnostic         `json:"store"`
	Requests      APIRequestDiagnostic    `json:"requests"`
}

type RuntimeBuildDiagnostic struct {
	Commit  string `json:"commit,omitempty"`
	BuiltAt string `json:"built_at,omitempty"`
}

type RuntimeTimingDiagnostic struct {
	TotalMS     int64                    `json:"total_ms"`
	TotalWarnMS int64                    `json:"total_warn_ms"`
	StageWarnMS int64                    `json:"stage_warn_ms"`
	Stages      []RuntimeStageDiagnostic `json:"stages"`
}

type RuntimeStageDiagnostic struct {
	Name       string `json:"name"`
	Status     Status `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Detail     string `json:"detail,omitempty"`
}

type RuntimeSettings struct {
	DeploymentMode        string
	ConfigPath            string
	DBPath                string
	GatusURL              string
	ListenAddr            string
	FrontendDir           string
	CacheTTL              time.Duration
	MetricsRetention      time.Duration
	ExpectedProductionURL string
	PublicDomain          string
	PublicIP              string
	TailnetStatusURL      string
	BuildCommit           string
	BuildTime             string
}

type DeploymentDiagnostic struct {
	Status Status            `json:"status"`
	Mode   string            `json:"mode"`
	Detail string            `json:"detail"`
	Checks []DeploymentCheck `json:"checks"`
}

type DeploymentCheck struct {
	Key      string `json:"key"`
	Category string `json:"category"`
	Status   Status `json:"status"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Detail   string `json:"detail"`
}

type ProjectDiagnostic struct {
	ProjectID               string           `json:"project_id"`
	ProjectName             string           `json:"project_name"`
	Status                  Status           `json:"status"`
	Detail                  string           `json:"detail"`
	ServiceCount            int              `json:"service_count"`
	CriticalServiceCount    int              `json:"critical_service_count"`
	DownServiceCount        int              `json:"down_service_count"`
	DegradedServiceCount    int              `json:"degraded_service_count"`
	EndpointCount           int              `json:"endpoint_count"`
	MissingEndpointCount    int              `json:"missing_endpoint_count"`
	UnmappedServiceCount    int              `json:"unmapped_service_count"`
	CheckCoveragePercent    float64          `json:"check_coverage_percent"`
	RelatedNodes            []string         `json:"related_nodes"`
	MetricsReportingNodes   []string         `json:"metrics_reporting_nodes"`
	MetricsMissingNodes     []string         `json:"metrics_missing_nodes"`
	MetricsStaleNodes       []string         `json:"metrics_stale_nodes"`
	RecentCheckCount        int              `json:"recent_check_count"`
	RecentSuccessCount      int              `json:"recent_success_count"`
	RecentFailureCount      int              `json:"recent_failure_count"`
	RecentFailurePercent    float64          `json:"recent_failure_percent"`
	NoRecentCheckCount      int              `json:"no_recent_check_count"`
	CurrentAvgResponseMS    float64          `json:"current_avg_response_time_ms"`
	CurrentMaxResponseMS    int64            `json:"current_max_response_time_ms"`
	LastCheckAt             *time.Time       `json:"last_check_at,omitempty"`
	RecentEventCount        int              `json:"recent_event_count"`
	LastEventAt             *time.Time       `json:"last_event_at,omitempty"`
	RecentEventKindCounts   []EventKindCount `json:"recent_event_kind_counts"`
	RecentEventStatusCounts StatusCounts     `json:"recent_event_status_counts"`
	OpsStatus               Status           `json:"ops_status"`
	OpsIssueCount           int              `json:"ops_issue_count"`
	OpsErrorCount           int              `json:"ops_error_count"`
	OpsWarnCount            int              `json:"ops_warn_count"`
	OpsInfoCount            int              `json:"ops_info_count"`
	OpsIssueKinds           []string         `json:"ops_issue_kinds,omitempty"`
	OpsAffectedNodes        []string         `json:"ops_affected_nodes,omitempty"`
	OpsAffectedServices     []string         `json:"ops_affected_services,omitempty"`
	OpsDetail               string           `json:"ops_detail"`
}

type AgentNodeDiagnostic struct {
	NodeID                   string                  `json:"node_id"`
	Hostname                 string                  `json:"hostname,omitempty"`
	Status                   Status                  `json:"status"`
	Detail                   string                  `json:"detail"`
	ReportCount              int                     `json:"report_count"`
	FailedReportCount        int                     `json:"failed_report_count"`
	CollectorFailureCount    int                     `json:"collector_failure_count"`
	LatestReceivedAt         *time.Time              `json:"latest_received_at,omitempty"`
	LatestCapturedAt         *time.Time              `json:"latest_captured_at,omitempty"`
	LatestReportLagSeconds   float64                 `json:"latest_report_lag_seconds"`
	ReportLagWarnSeconds     float64                 `json:"report_lag_warn_seconds"`
	ReportLagHeadroomSeconds float64                 `json:"report_lag_headroom_seconds"`
	LatestSchemaVersion      int                     `json:"latest_schema_version"`
	LatestCollectorOK        int                     `json:"latest_collector_ok"`
	LatestCollectorFailed    int                     `json:"latest_collector_failed"`
	GPUAvailable             bool                    `json:"gpu_available"`
	StorageDeviceCount       int                     `json:"storage_device_count"`
	NetworkInterfaceCount    int                     `json:"network_interface_count"`
	LatestFailedCollectors   []AgentCollectorFailure `json:"latest_failed_collectors,omitempty"`
}

type AgentCollectorFailure struct {
	Name            string  `json:"name"`
	Detail          string  `json:"detail,omitempty"`
	ElapsedMS       int64   `json:"elapsed_ms,omitempty"`
	Cached          bool    `json:"cached,omitempty"`
	CacheAgeSeconds float64 `json:"cache_age_seconds,omitempty"`
}

type RuntimeMemoryDiagnostic struct {
	AllocBytes     uint64  `json:"alloc_bytes"`
	SysBytes       uint64  `json:"sys_bytes"`
	HeapAllocBytes uint64  `json:"heap_alloc_bytes"`
	HeapInuseBytes uint64  `json:"heap_inuse_bytes"`
	NumGC          uint32  `json:"num_gc"`
	LastGCPauseMS  float64 `json:"last_gc_pause_ms"`
}

type SummaryCacheDiagnostic struct {
	TTLSeconds         float64    `json:"ttl_seconds"`
	Cached             bool       `json:"cached"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	SecondsUntilExpiry float64    `json:"seconds_until_expiry"`
}

type StoreDiagnostic struct {
	Path                   string     `json:"path"`
	DBSizeBytes            int64      `json:"db_size_bytes"`
	WALSizeBytes           int64      `json:"wal_size_bytes"`
	SHMSizeBytes           int64      `json:"shm_size_bytes"`
	TotalSizeBytes         int64      `json:"total_size_bytes"`
	StatusCacheRows        int        `json:"status_cache_rows"`
	EventRows              int        `json:"event_rows"`
	MetricsLatestRows      int        `json:"metrics_latest_rows"`
	MetricsSampleRows      int        `json:"metrics_sample_rows"`
	MetricsReportLogRows   int        `json:"metrics_report_log_rows"`
	LatestMetricAt         *time.Time `json:"latest_metric_at,omitempty"`
	LatestReportAt         *time.Time `json:"latest_report_at,omitempty"`
	MetricsRetentionDays   float64    `json:"metrics_retention_days"`
	ReportLogRetentionDays float64    `json:"report_log_retention_days"`
	ReportLogLimit         int        `json:"report_log_limit"`
}

type APIRequestDiagnostic struct {
	Since               time.Time                   `json:"since,omitempty"`
	Total               int64                       `json:"total"`
	StatusCounts        APIRequestStatusCounts      `json:"status_counts"`
	SlowThresholdMS     float64                     `json:"slow_threshold_ms"`
	SlowCount           int64                       `json:"slow_count"`
	AvgDurationMS       float64                     `json:"avg_duration_ms"`
	MaxDurationMS       float64                     `json:"max_duration_ms"`
	RecentSampleLimit   int                         `json:"recent_sample_limit"`
	RecentP95DurationMS float64                     `json:"recent_p95_duration_ms"`
	RecentMaxDurationMS float64                     `json:"recent_max_duration_ms"`
	Recent              []APIRequestSample          `json:"recent"`
	Routes              []APIRequestRouteDiagnostic `json:"routes"`
}

type APIRequestStatusCounts struct {
	Success     int64 `json:"success"`
	Redirect    int64 `json:"redirect"`
	ClientError int64 `json:"client_error"`
	ServerError int64 `json:"server_error"`
	Other       int64 `json:"other"`
}

type APIRequestSample struct {
	Method     string    `json:"method"`
	Route      string    `json:"route"`
	Status     int       `json:"status"`
	Bytes      int64     `json:"bytes"`
	DurationMS float64   `json:"duration_ms"`
	At         time.Time `json:"at"`
}

type APIRequestRouteDiagnostic struct {
	Method         string                 `json:"method"`
	Route          string                 `json:"route"`
	Total          int64                  `json:"total"`
	StatusCounts   APIRequestStatusCounts `json:"status_counts"`
	AvgDurationMS  float64                `json:"avg_duration_ms"`
	MaxDurationMS  float64                `json:"max_duration_ms"`
	SlowCount      int64                  `json:"slow_count"`
	LastStatus     int                    `json:"last_status"`
	LastDurationMS float64                `json:"last_duration_ms"`
	LastSeenAt     time.Time              `json:"last_seen_at,omitempty"`
}

type OpsDiagnostic struct {
	Issues             []OpsIssue                   `json:"issues"`
	Counts             OpsIssueCounts               `json:"counts"`
	ProjectImpacts     []OpsProjectImpact           `json:"project_impacts"`
	StatusVolatility   StatusVolatilityDiagnostic   `json:"status_volatility"`
	ResourceThresholds []EffectiveResourceThreshold `json:"resource_thresholds"`
	ResourceStates     []OpsResourceState           `json:"resource_states"`
}

type OpsIssueCounts struct {
	Error int `json:"error"`
	Warn  int `json:"warn"`
	Info  int `json:"info"`
}

type OpsIssue struct {
	Severity    string    `json:"severity"`
	Kind        string    `json:"kind"`
	SubjectID   string    `json:"subject_id"`
	SubjectName string    `json:"subject_name,omitempty"`
	NodeID      string    `json:"node_id,omitempty"`
	ProjectID   string    `json:"project_id,omitempty"`
	ServiceID   string    `json:"service_id,omitempty"`
	Status      Status    `json:"status,omitempty"`
	Metric      string    `json:"metric,omitempty"`
	Value       float64   `json:"value,omitempty"`
	Limit       float64   `json:"limit,omitempty"`
	Unit        string    `json:"unit,omitempty"`
	Detail      string    `json:"detail"`
	ObservedAt  time.Time `json:"observed_at,omitempty"`
}

type OpsProjectImpact struct {
	ProjectID        string   `json:"project_id"`
	ProjectName      string   `json:"project_name"`
	Status           Status   `json:"status"`
	IssueCount       int      `json:"issue_count"`
	ErrorCount       int      `json:"error_count"`
	WarnCount        int      `json:"warn_count"`
	InfoCount        int      `json:"info_count"`
	AffectedNodes    []string `json:"affected_nodes,omitempty"`
	AffectedServices []string `json:"affected_services,omitempty"`
	IssueKinds       []string `json:"issue_kinds,omitempty"`
	Detail           string   `json:"detail"`
}

type StatusVolatilityDiagnostic struct {
	WindowSeconds   float64                   `json:"window_seconds"`
	ChangeThreshold int                       `json:"change_threshold"`
	Subjects        []StatusVolatilitySubject `json:"subjects"`
}

type StatusVolatilitySubject struct {
	Kind         string    `json:"kind"`
	SubjectID    string    `json:"subject_id"`
	Label        string    `json:"label"`
	ChangeCount  int       `json:"change_count"`
	Status       Status    `json:"status"`
	LatestFrom   Status    `json:"latest_from,omitempty"`
	LatestTo     Status    `json:"latest_to"`
	LatestDetail string    `json:"latest_detail"`
	LatestAt     time.Time `json:"latest_at"`
	Detail       string    `json:"detail"`
}

type EffectiveResourceThreshold struct {
	NodeID          string  `json:"node_id"`
	CPUPercent      float64 `json:"cpu_percent"`
	MemoryPercent   float64 `json:"memory_percent"`
	RootDiskPercent float64 `json:"root_disk_percent"`
	GPUUtilPercent  float64 `json:"gpu_util_percent"`
	NetworkRXBps    float64 `json:"network_rx_bps,omitempty"`
	NetworkTXBps    float64 `json:"network_tx_bps,omitempty"`
	StorageReadBps  float64 `json:"storage_read_bps,omitempty"`
	StorageWriteBps float64 `json:"storage_write_bps,omitempty"`
}

type OpsResourceState struct {
	NodeID       string           `json:"node_id"`
	Status       Status           `json:"status"`
	Detail       string           `json:"detail"`
	ObservedAt   time.Time        `json:"observed_at,omitempty"`
	Stale        bool             `json:"stale"`
	CPU          ResourceHeadroom `json:"cpu"`
	Memory       ResourceHeadroom `json:"memory"`
	RootDisk     ResourceHeadroom `json:"root_disk"`
	GPUAvailable bool             `json:"gpu_available"`
	GPUName      string           `json:"gpu_name,omitempty"`
	GPU          ResourceHeadroom `json:"gpu"`
	NetworkRX    ResourceHeadroom `json:"network_rx"`
	NetworkTX    ResourceHeadroom `json:"network_tx"`
	StorageRead  ResourceHeadroom `json:"storage_read"`
	StorageWrite ResourceHeadroom `json:"storage_write"`
}

type ResourceHeadroom struct {
	Configured bool    `json:"configured"`
	Value      float64 `json:"value"`
	Limit      float64 `json:"limit,omitempty"`
	Headroom   float64 `json:"headroom,omitempty"`
	Unit       string  `json:"unit"`
}

type StatusCounts struct {
	OK          int `json:"ok"`
	Degraded    int `json:"degraded"`
	Down        int `json:"down"`
	Unknown     int `json:"unknown"`
	Maintenance int `json:"maintenance"`
}

type Event struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	SubjectID string    `json:"subject_id"`
	Label     string    `json:"label"`
	From      Status    `json:"from,omitempty"`
	To        Status    `json:"to"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

type EventLogDiagnostic struct {
	Total        int              `json:"total"`
	Returned     int              `json:"returned"`
	LatestAt     *time.Time       `json:"latest_at,omitempty"`
	KindCounts   []EventKindCount `json:"kind_counts"`
	StatusCounts StatusCounts     `json:"status_counts"`
	Events       []Event          `json:"events"`
}

type EventKindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type MetricsReport struct {
	NodeID     string            `json:"node_id"`
	Hostname   string            `json:"hostname"`
	CapturedAt time.Time         `json:"captured_at"`
	CPU        CPUMetrics        `json:"cpu"`
	Memory     MemoryMetrics     `json:"memory"`
	Swap       MemoryMetrics     `json:"swap"`
	Disk       DiskMetrics       `json:"disk"`
	Network    NetworkMetrics    `json:"network"`
	Uptime     UptimeMetrics     `json:"uptime"`
	Extra      map[string]string `json:"extra,omitempty"`
}

type MetricsReportV2 struct {
	SchemaVersion   int               `json:"schema_version"`
	NodeID          string            `json:"node_id"`
	Hostname        string            `json:"hostname"`
	CapturedAt      time.Time         `json:"captured_at"`
	Resources       MetricsResources  `json:"resources"`
	CollectorStatus []CollectorStatus `json:"collector_status,omitempty"`
	Extra           map[string]string `json:"extra,omitempty"`
}

type MetricsResources struct {
	CPU        CPUMetricsV2     `json:"cpu"`
	Memory     MemoryMetrics    `json:"memory"`
	Swap       MemoryMetrics    `json:"swap"`
	Disk       DiskMetrics      `json:"disk"`
	Storage    StorageMetrics   `json:"storage"`
	Network    NetworkMetricsV2 `json:"network"`
	GPU        GPUMetrics       `json:"gpu"`
	Containers ContainerMetrics `json:"containers"`
	Processes  ProcessMetrics   `json:"processes"`
	Uptime     UptimeMetrics    `json:"uptime"`
}

type MetricsView struct {
	MetricsReport
	SchemaVersion   int               `json:"schema_version"`
	Storage         StorageMetrics    `json:"storage"`
	GPU             GPUMetrics        `json:"gpu"`
	Containers      ContainerMetrics  `json:"containers"`
	Processes       ProcessMetrics    `json:"processes"`
	CollectorStatus []CollectorStatus `json:"collector_status,omitempty"`
	UpdatedAt       time.Time         `json:"updated_at"`
	Stale           bool              `json:"stale"`
}

type MetricsHistoryResponse struct {
	NodeID   string                `json:"node_id"`
	Window   string                `json:"window"`
	Summary  MetricsHistorySummary `json:"summary"`
	Points   []MetricsHistoryPoint `json:"points"`
	Returned int                   `json:"returned"`
}

type ProjectMetricsHistoryResponse struct {
	GeneratedAt time.Time                   `json:"generated_at"`
	ProjectID   string                      `json:"project_id"`
	Window      string                      `json:"window"`
	Nodes       []ProjectNodeMetricsHistory `json:"nodes"`
	Returned    int                         `json:"returned"`
}

type ProjectNodeMetricsHistory struct {
	NodeID  string                `json:"node_id"`
	Summary MetricsHistorySummary `json:"summary"`
	Points  []MetricsHistoryPoint `json:"points"`
}

type MetricsHistorySummary struct {
	Samples             int     `json:"samples"`
	MaxCPUPercent       float64 `json:"max_cpu_percent"`
	MaxMemoryPercent    float64 `json:"max_memory_percent"`
	MaxDiskPercent      float64 `json:"max_disk_percent"`
	MaxNetworkRXBps     float64 `json:"max_network_rx_bps"`
	MaxNetworkTXBps     float64 `json:"max_network_tx_bps"`
	MaxStorageReadBps   float64 `json:"max_storage_read_bps"`
	MaxStorageWriteBps  float64 `json:"max_storage_write_bps"`
	MaxStorageReadIOPS  float64 `json:"max_storage_read_iops"`
	MaxStorageWriteIOPS float64 `json:"max_storage_write_iops"`
	GPUAvailable        bool    `json:"gpu_available"`
	MaxGPUPercent       float64 `json:"max_gpu_percent"`
}

type ProjectMetricsSummary = MetricsHistorySummary

type MetricsReportLogsResponse struct {
	GeneratedAt time.Time          `json:"generated_at"`
	NodeID      string             `json:"node_id,omitempty"`
	Logs        []MetricsReportLog `json:"logs"`
	Returned    int                `json:"returned"`
}

type MetricsReportLog struct {
	ID                    int64             `json:"id"`
	NodeID                string            `json:"node_id"`
	Hostname              string            `json:"hostname"`
	SchemaVersion         int               `json:"schema_version"`
	CapturedAt            time.Time         `json:"captured_at"`
	ReceivedAt            time.Time         `json:"received_at"`
	ReportLagSeconds      float64           `json:"report_lag_seconds"`
	CollectorOK           int               `json:"collector_ok"`
	CollectorFailed       int               `json:"collector_failed"`
	CollectorStatus       []CollectorStatus `json:"collector_status,omitempty"`
	GPUAvailable          bool              `json:"gpu_available"`
	StorageDeviceCount    int               `json:"storage_device_count"`
	NetworkInterfaceCount int               `json:"network_interface_count"`
}

type MetricsHistoryPoint struct {
	NodeID           string    `json:"node_id"`
	CapturedAt       time.Time `json:"captured_at"`
	CPUPercent       float64   `json:"cpu_percent"`
	MemoryPercent    float64   `json:"memory_percent"`
	DiskPercent      float64   `json:"disk_percent"`
	NetworkRXBps     float64   `json:"network_rx_bps"`
	NetworkTXBps     float64   `json:"network_tx_bps"`
	StorageReadBps   float64   `json:"storage_read_bps"`
	StorageWriteBps  float64   `json:"storage_write_bps"`
	StorageReadIOPS  float64   `json:"storage_read_iops"`
	StorageWriteIOPS float64   `json:"storage_write_iops"`
	GPUAvailable     bool      `json:"gpu_available"`
	GPUPercent       float64   `json:"gpu_percent"`
}

type TelemetrySchemaResponse struct {
	Version int               `json:"version"`
	Domains []TelemetryDomain `json:"domains"`
}

type TelemetryDomain struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Optional bool     `json:"optional"`
	Fields   []string `json:"fields"`
}

type CPUMetrics struct {
	Percent         float64   `json:"percent"`
	Load1           float64   `json:"load1"`
	Load5           float64   `json:"load5"`
	Load15          float64   `json:"load15"`
	PerCorePercent  []float64 `json:"per_core_percent,omitempty"`
	ContextSwitches int64     `json:"context_switches,omitempty"`
	Interrupts      int64     `json:"interrupts,omitempty"`
}

type CPUMetricsV2 struct {
	CPUMetrics
}

type MemoryMetrics struct {
	TotalBytes int64   `json:"total_bytes"`
	UsedBytes  int64   `json:"used_bytes"`
	Percent    float64 `json:"percent"`
}

type DiskMetrics struct {
	Mountpoint string  `json:"mountpoint"`
	TotalBytes int64   `json:"total_bytes"`
	UsedBytes  int64   `json:"used_bytes"`
	Percent    float64 `json:"percent"`
}

type StorageMetrics struct {
	ReadBytes  int64                 `json:"read_bytes"`
	WriteBytes int64                 `json:"write_bytes"`
	ReadBps    float64               `json:"read_bps"`
	WriteBps   float64               `json:"write_bps"`
	ReadIOPS   float64               `json:"read_iops"`
	WriteIOPS  float64               `json:"write_iops"`
	Devices    []StorageDeviceMetric `json:"devices,omitempty"`
}

type StorageDeviceMetric struct {
	Name       string `json:"name"`
	ReadBytes  int64  `json:"read_bytes"`
	WriteBytes int64  `json:"write_bytes"`
	ReadIOs    int64  `json:"read_ios"`
	WriteIOs   int64  `json:"write_ios"`
}

type NetworkMetrics struct {
	RXBytes    int64                    `json:"rx_bytes"`
	TXBytes    int64                    `json:"tx_bytes"`
	RXBps      float64                  `json:"rx_bps"`
	TXBps      float64                  `json:"tx_bps"`
	Interfaces []NetworkInterfaceMetric `json:"interfaces"`
}

type NetworkInterfaceMetric struct {
	Name      string `json:"name"`
	RXBytes   int64  `json:"rx_bytes"`
	TXBytes   int64  `json:"tx_bytes"`
	RXPackets int64  `json:"rx_packets"`
	TXPackets int64  `json:"tx_packets"`
	RXErrors  int64  `json:"rx_errors"`
	TXErrors  int64  `json:"tx_errors"`
	RXDrops   int64  `json:"rx_drops"`
	TXDrops   int64  `json:"tx_drops"`
}

type NetworkMetricsV2 struct {
	RXBytes    int64                    `json:"rx_bytes"`
	TXBytes    int64                    `json:"tx_bytes"`
	RXBps      float64                  `json:"rx_bps"`
	TXBps      float64                  `json:"tx_bps"`
	Interfaces []NetworkInterfaceMetric `json:"interfaces"`
}

type NetworkInterfaceMetricV2 = NetworkInterfaceMetric

type GPUMetrics struct {
	Available bool              `json:"available"`
	Provider  string            `json:"provider"`
	Devices   []GPUDeviceMetric `json:"devices,omitempty"`
}

type GPUDeviceMetric struct {
	Index         string  `json:"index"`
	Name          string  `json:"name"`
	UtilPercent   float64 `json:"util_percent"`
	MemoryTotal   int64   `json:"memory_total_bytes"`
	MemoryUsed    int64   `json:"memory_used_bytes"`
	MemoryPercent float64 `json:"memory_percent"`
	TemperatureC  float64 `json:"temperature_c"`
	PowerWatts    float64 `json:"power_watts"`
}

type ContainerMetrics struct {
	Available  bool              `json:"available"`
	Provider   string            `json:"provider"`
	Containers []ContainerMetric `json:"containers,omitempty"`
}

type ContainerMetric struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Image            string  `json:"image"`
	State            string  `json:"state"`
	ComposeProject   string  `json:"compose_project,omitempty"`
	CPUPercent       float64 `json:"cpu_percent"`
	MemoryPercent    float64 `json:"memory_percent"`
	MemoryUsageBytes int64   `json:"memory_usage_bytes"`
	MemoryLimitBytes int64   `json:"memory_limit_bytes"`
	NetworkRXBytes   int64   `json:"network_rx_bytes"`
	NetworkTXBytes   int64   `json:"network_tx_bytes"`
	BlockReadBytes   int64   `json:"block_read_bytes"`
	BlockWriteBytes  int64   `json:"block_write_bytes"`
}

type ProcessMetrics struct {
	ProcessCount int             `json:"process_count"`
	Processes    []ProcessMetric `json:"processes,omitempty"`
}

type ProcessMetric struct {
	PID           int     `json:"pid"`
	PPID          int     `json:"ppid"`
	User          string  `json:"user"`
	Command       string  `json:"command"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	RSSBytes      int64   `json:"rss_bytes"`
}

type CollectorStatus struct {
	Name            string  `json:"name"`
	OK              bool    `json:"ok"`
	Detail          string  `json:"detail,omitempty"`
	ElapsedMS       int64   `json:"elapsed_ms,omitempty"`
	Cached          bool    `json:"cached,omitempty"`
	CacheAgeSeconds float64 `json:"cache_age_seconds,omitempty"`
}

type UptimeMetrics struct {
	Seconds float64 `json:"seconds"`
}
