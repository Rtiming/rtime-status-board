package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Aggregator struct {
	config    *AppConfig
	store     *Store
	gatus     *GatusClient
	ttl       time.Duration
	startedAt time.Time
	runtime   RuntimeSettings

	mu        sync.Mutex
	expiresAt time.Time
	cached    *SummaryResponse
}

const (
	defaultCPUThresholdPercent      = 90
	defaultMemoryThresholdPercent   = 90
	defaultRootDiskThresholdPercent = 85
	defaultGPUUtilThresholdPercent  = 90
	statusVolatilityWindow          = 24 * time.Hour
	statusVolatilityThreshold       = 3
	statusVolatilityLimit           = 20
	diagnosticsTotalWarnMS          = 1500
	diagnosticsStageWarnMS          = 1000
	agentReportLagWarnSeconds       = 30
)

func NewAggregator(config *AppConfig, store *Store, gatus *GatusClient, ttl time.Duration) *Aggregator {
	return NewAggregatorWithRuntime(config, store, gatus, ttl, RuntimeSettings{})
}

func NewAggregatorWithRuntime(config *AppConfig, store *Store, gatus *GatusClient, ttl time.Duration, runtimeSettings RuntimeSettings) *Aggregator {
	return &Aggregator{config: config, store: store, gatus: gatus, ttl: ttl, runtime: runtimeSettings, startedAt: time.Now().UTC()}
}

func (a *Aggregator) Summary(ctx context.Context) (*SummaryResponse, error) {
	a.mu.Lock()
	if a.cached != nil && time.Now().Before(a.expiresAt) {
		cached := *a.cached
		a.mu.Unlock()
		return &cached, nil
	}
	a.mu.Unlock()

	checks, err := a.gatus.Statuses(ctx)
	if err != nil {
		checks = map[string]RuntimeCheck{}
	}

	services := a.serviceViews(checks, err)
	nodes := a.nodeViews(services)
	projects := a.projectViews(services)
	overall := aggregateOverall(projectStatuses(projects))
	counts := countStatuses(services)

	for _, service := range services {
		_ = a.store.RecordStatus(ctx, "service", service.ID, service.Name, service.Status, service.Detail)
	}
	for _, node := range nodes {
		_ = a.store.RecordStatus(ctx, "node", node.ID, node.Name, node.Status, node.Detail)
	}
	for _, project := range projects {
		_ = a.store.RecordStatus(ctx, "project", project.ID, project.Name, project.Status, project.Detail)
	}

	events, eventErr := a.store.RecentEvents(ctx, 50)
	if eventErr != nil {
		events = []Event{}
	}
	metrics, metricsErr := a.store.LatestMetrics(ctx)
	if metricsErr != nil {
		metrics = []MetricsView{}
	}

	summary := &SummaryResponse{
		App:         a.config.App,
		GeneratedAt: time.Now().UTC(),
		Overall:     overall,
		Counts:      counts,
		Nodes:       nodes,
		Projects:    projects,
		Services:    services,
		Metrics:     metrics,
		Events:      events,
	}

	a.mu.Lock()
	a.cached = summary
	a.expiresAt = time.Now().Add(a.ttl)
	a.mu.Unlock()

	return summary, nil
}

func (a *Aggregator) Checks(ctx context.Context) ([]RuntimeEndpointStatus, error) {
	checks, err := a.gatus.EndpointStatuses(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(checks, func(i, j int) bool {
		if checks[i].Group == checks[j].Group {
			return checks[i].Name < checks[j].Name
		}
		return checks[i].Group < checks[j].Group
	})
	return checks, nil
}

func (a *Aggregator) ProjectDetail(ctx context.Context, projectID string) (*ProjectDetailResponse, error) {
	summary, err := a.Summary(ctx)
	if err != nil {
		return nil, err
	}
	detail, err := projectDetailFromSummary(summary, projectID)
	if err != nil {
		return nil, err
	}
	detail.ResourceStates = a.resourceStates(summary.GeneratedAt, detail.Metrics)
	return detail, nil
}

func (a *Aggregator) ProjectMetricsHistory(ctx context.Context, projectID string, window time.Duration, limit int) (*ProjectMetricsHistoryResponse, error) {
	summary, err := a.Summary(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := projectFromSummary(summary, projectID); err != nil {
		return nil, err
	}
	if window <= 0 {
		window = time.Hour
	}
	if limit <= 0 || limit > 3000 {
		limit = 3000
	}

	nodeIDs := projectNodeIDsFromSummary(summary, projectID)
	response := &ProjectMetricsHistoryResponse{
		GeneratedAt: time.Now().UTC(),
		ProjectID:   projectID,
		Window:      window.String(),
		Nodes:       make([]ProjectNodeMetricsHistory, 0, len(nodeIDs)),
	}
	since := time.Now().UTC().Add(-window)
	for _, nodeID := range nodeIDs {
		points, err := a.store.MetricsHistory(ctx, nodeID, since, limit)
		if err != nil {
			return nil, err
		}
		response.Nodes = append(response.Nodes, ProjectNodeMetricsHistory{
			NodeID:  nodeID,
			Summary: summarizeMetricsHistory(points),
			Points:  points,
		})
		response.Returned += len(points)
	}
	return response, nil
}

func (a *Aggregator) ProjectChecks(ctx context.Context, projectID string, window time.Duration, limit int) (*ProjectCheckHistoryResponse, error) {
	summary, err := a.Summary(ctx)
	if err != nil {
		return nil, err
	}
	project, err := projectFromSummary(summary, projectID)
	if err != nil {
		return nil, err
	}
	if window <= 0 {
		window = 24 * time.Hour
	}
	if limit <= 0 || limit > 200 {
		limit = 60
	}

	resultsByEndpoint, err := a.gatus.EndpointResultsMap(ctx)
	if err != nil {
		return nil, err
	}
	since := time.Now().UTC().Add(-window)
	services := projectServicesFromSummary(summary, project.ProjectConfig)
	results := []ProjectCheckResult{}
	endpointCount := 0
	for _, service := range services {
		if service.EndpointKey == "" {
			continue
		}
		endpointCount++
		serviceResults := resultsByEndpoint[service.EndpointKey]
		for _, result := range serviceResults {
			if !result.Timestamp.IsZero() && result.Timestamp.Before(since) {
				continue
			}
			results = append(results, ProjectCheckResult{
				ServiceID:      service.ID,
				ServiceName:    service.Name,
				NodeID:         service.NodeID,
				EndpointKey:    service.EndpointKey,
				Timestamp:      result.Timestamp,
				Status:         result.Status,
				Success:        result.Success,
				Detail:         result.Detail,
				ResponseTimeMS: result.ResponseTimeMS,
				Errors:         result.Errors,
				Conditions:     result.Conditions,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Timestamp.Equal(results[j].Timestamp) {
			if results[i].NodeID == results[j].NodeID {
				return results[i].ServiceID < results[j].ServiceID
			}
			return results[i].NodeID < results[j].NodeID
		}
		return results[i].Timestamp.After(results[j].Timestamp)
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return &ProjectCheckHistoryResponse{
		GeneratedAt:   time.Now().UTC(),
		Project:       project,
		Window:        window.String(),
		EndpointCount: endpointCount,
		Summary:       summarizeCheckHistory(results, func(result ProjectCheckResult) bool { return result.Success }, func(result ProjectCheckResult) time.Time { return result.Timestamp }, func(result ProjectCheckResult) int64 { return result.ResponseTimeMS }),
		Results:       results,
		Returned:      len(results),
	}, nil
}

func (a *Aggregator) NodeDetail(ctx context.Context, nodeID string) (*NodeDetailResponse, error) {
	summary, err := a.Summary(ctx)
	if err != nil {
		return nil, err
	}
	detail, err := nodeDetailFromSummary(summary, nodeID)
	if err != nil {
		return nil, err
	}
	if detail.Metrics != nil {
		detail.ResourceStates = a.resourceStates(summary.GeneratedAt, []MetricsView{*detail.Metrics})
	}
	return detail, nil
}

func (a *Aggregator) NodeChecks(ctx context.Context, nodeID string, window time.Duration, limit int) (*NodeCheckHistoryResponse, error) {
	summary, err := a.Summary(ctx)
	if err != nil {
		return nil, err
	}
	node, err := nodeFromSummary(summary, nodeID)
	if err != nil {
		return nil, err
	}
	if window <= 0 {
		window = 24 * time.Hour
	}
	if limit <= 0 || limit > 200 {
		limit = 60
	}

	resultsByEndpoint, err := a.gatus.EndpointResultsMap(ctx)
	if err != nil {
		return nil, err
	}
	since := time.Now().UTC().Add(-window)
	results := []NodeCheckResult{}
	endpointCount := 0
	for _, service := range summary.Services {
		if service.NodeID != node.ID || service.EndpointKey == "" {
			continue
		}
		endpointCount++
		for _, result := range resultsByEndpoint[service.EndpointKey] {
			if !result.Timestamp.IsZero() && result.Timestamp.Before(since) {
				continue
			}
			results = append(results, NodeCheckResult{
				ServiceID:      service.ID,
				ServiceName:    service.Name,
				ProjectID:      service.ProjectID,
				EndpointKey:    service.EndpointKey,
				Timestamp:      result.Timestamp,
				Status:         result.Status,
				Success:        result.Success,
				Detail:         result.Detail,
				ResponseTimeMS: result.ResponseTimeMS,
				Errors:         result.Errors,
				Conditions:     result.Conditions,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Timestamp.Equal(results[j].Timestamp) {
			return results[i].ServiceID < results[j].ServiceID
		}
		return results[i].Timestamp.After(results[j].Timestamp)
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return &NodeCheckHistoryResponse{
		GeneratedAt:   time.Now().UTC(),
		Node:          node,
		Window:        window.String(),
		EndpointCount: endpointCount,
		Summary:       summarizeCheckHistory(results, func(result NodeCheckResult) bool { return result.Success }, func(result NodeCheckResult) time.Time { return result.Timestamp }, func(result NodeCheckResult) int64 { return result.ResponseTimeMS }),
		Results:       results,
		Returned:      len(results),
	}, nil
}

func (a *Aggregator) ServiceDetail(ctx context.Context, serviceID string) (*ServiceDetailResponse, error) {
	summary, err := a.Summary(ctx)
	if err != nil {
		return nil, err
	}
	return serviceDetailFromSummary(summary, serviceID)
}

func (a *Aggregator) ServiceChecks(ctx context.Context, serviceID string, window time.Duration, limit int) (*ServiceCheckHistoryResponse, error) {
	summary, err := a.Summary(ctx)
	if err != nil {
		return nil, err
	}
	service, err := serviceFromSummary(summary, serviceID)
	if err != nil {
		return nil, err
	}
	if service.EndpointKey == "" {
		return &ServiceCheckHistoryResponse{
			GeneratedAt: summary.GeneratedAt,
			Service:     service,
			Window:      window.String(),
			Summary:     CheckHistorySummary{},
			Results:     []ServiceCheckResult{},
		}, nil
	}

	results, err := a.gatus.EndpointResults(ctx, service.EndpointKey)
	if err != nil {
		return nil, err
	}
	if window <= 0 {
		window = 24 * time.Hour
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	since := time.Now().UTC().Add(-window)
	filtered := make([]ServiceCheckResult, 0, len(results))
	for i := len(results) - 1; i >= 0; i-- {
		result := results[i]
		if !result.Timestamp.IsZero() && result.Timestamp.Before(since) {
			continue
		}
		filtered = append(filtered, result)
		if len(filtered) >= limit {
			break
		}
	}

	return &ServiceCheckHistoryResponse{
		GeneratedAt: summary.GeneratedAt,
		Service:     service,
		EndpointKey: service.EndpointKey,
		Window:      window.String(),
		Summary:     summarizeCheckHistory(filtered, func(result ServiceCheckResult) bool { return result.Success }, func(result ServiceCheckResult) time.Time { return result.Timestamp }, func(result ServiceCheckResult) int64 { return result.ResponseTimeMS }),
		Results:     filtered,
		Returned:    len(filtered),
	}, nil
}

func summarizeCheckHistory[T any](results []T, success func(T) bool, timestamp func(T) time.Time, responseTimeMS func(T) int64) CheckHistorySummary {
	summary := CheckHistorySummary{Total: len(results)}
	if len(results) == 0 {
		return summary
	}
	latencies := make([]int64, 0, len(results))
	var totalLatency int64
	for _, result := range results {
		if success(result) {
			summary.Successes++
		} else {
			summary.Failures++
			ts := timestamp(result)
			if !ts.IsZero() && (summary.LastFailureAt == nil || ts.After(*summary.LastFailureAt)) {
				copy := ts
				summary.LastFailureAt = &copy
			}
		}
		latency := responseTimeMS(result)
		if latency < 0 {
			latency = 0
		}
		latencies = append(latencies, latency)
		totalLatency += latency
		if latency > summary.MaxResponseTimeMS {
			summary.MaxResponseTimeMS = latency
		}
	}
	summary.FailurePercent = float64(summary.Failures) / float64(summary.Total) * 100
	summary.AvgResponseTimeMS = float64(totalLatency) / float64(len(latencies))
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p95Index := int(math.Ceil(float64(len(latencies))*0.95)) - 1
	if p95Index < 0 {
		p95Index = 0
	}
	if p95Index >= len(latencies) {
		p95Index = len(latencies) - 1
	}
	summary.P95ResponseTimeMS = latencies[p95Index]
	return summary
}

func (a *Aggregator) Diagnostics(ctx context.Context) (*DiagnosticsResponse, error) {
	diagnosticsStarted := time.Now()
	generatedAt := diagnosticsStarted.UTC()
	timing := RuntimeTimingDiagnostic{
		TotalWarnMS: diagnosticsTotalWarnMS,
		StageWarnMS: diagnosticsStageWarnMS,
		Stages:      []RuntimeStageDiagnostic{},
	}
	recordStage := func(name string, started time.Time, status Status, detail string) {
		timing.Stages = append(timing.Stages, RuntimeStageDiagnostic{
			Name:       name,
			Status:     status,
			DurationMS: time.Since(started).Milliseconds(),
			Detail:     detail,
		})
	}

	gatusStart := time.Now()
	endpoints, gatusErr := a.gatus.EndpointStatuses(ctx)
	gatusLatency := time.Since(gatusStart)
	recordStage("gatus-endpoints", gatusStart, providerStatus(gatusErr == nil, len(endpoints) > 0), providerDetail(gatusErr, fmt.Sprintf("%d endpoints loaded", len(endpoints))))

	checks := map[string]RuntimeCheck{}
	if gatusErr == nil {
		for _, endpoint := range endpoints {
			checks[endpoint.Key] = RuntimeCheck{
				Key:            endpoint.Key,
				Status:         endpoint.Status,
				Success:        endpoint.Success,
				Detail:         endpoint.Detail,
				ResponseTimeMS: endpoint.ResponseTimeMS,
				LastCheckedAt:  endpoint.LastCheckedAt,
			}
		}
	}

	services := a.serviceViews(checks, gatusErr)
	projects := a.projectViews(services)
	overall := aggregateOverall(projectStatuses(projects))
	counts := countStatuses(services)

	metricsStart := time.Now()
	metrics, metricsErr := a.store.LatestMetrics(ctx)
	recordStage("sqlite-latest-metrics", metricsStart, providerStatus(metricsErr == nil, true), providerDetail(metricsErr, fmt.Sprintf("%d metrics rows", len(metrics))))
	metricsDiagnostic := a.metricsDiagnostic(metrics)
	agentReportsStart := time.Now()
	agentReports, agentReportsErr := a.store.RecentMetricReports(ctx, "", 20)
	if agentReportsErr != nil {
		agentReports = []MetricsReportLog{}
	}
	recordStage("sqlite-agent-reports", agentReportsStart, providerStatus(agentReportsErr == nil, true), providerDetail(agentReportsErr, fmt.Sprintf("%d report logs", len(agentReports))))
	recentEventsStart := time.Now()
	recentEvents, recentEventsErr := a.store.RecentEvents(ctx, 20)
	if recentEventsErr != nil {
		recentEvents = []Event{}
	}
	recordStage("sqlite-recent-events", recentEventsStart, providerStatus(recentEventsErr == nil, true), providerDetail(recentEventsErr, fmt.Sprintf("%d events", len(recentEvents))))
	volatilityStart := time.Now()
	statusVolatility, statusVolatilityErr := a.statusVolatilityDiagnostic(ctx, generatedAt)
	recordStage("sqlite-status-volatility", volatilityStart, providerStatus(statusVolatilityErr == nil, true), providerDetail(statusVolatilityErr, fmt.Sprintf("%d volatile subjects", len(statusVolatility.Subjects))))
	storeStart := time.Now()
	storeDiagnostic, storeDiagnosticErr := a.storeDiagnostic(ctx)
	recordStage("sqlite-store-diagnostics", storeStart, providerStatus(storeDiagnosticErr == nil, true), providerDetail(storeDiagnosticErr, fmt.Sprintf("%d event rows", storeDiagnostic.EventRows)))
	issues := a.configIssues(endpoints, gatusErr)
	failures := failingServices(services)
	opsStart := time.Now()
	ops := a.opsDiagnostic(generatedAt, services, metrics, metricsDiagnostic, issues, statusVolatility)
	recordStage("ops-rollup", opsStart, StatusOK, fmt.Sprintf("%d ops issues", len(ops.Issues)))
	deploymentStart := time.Now()
	deployment := a.deploymentDiagnostic(storeDiagnostic)
	recordStage("deployment-checks", deploymentStart, deployment.Status, fmt.Sprintf("%d deployment checks", len(deployment.Checks)))
	projectDiagnosticsStart := time.Now()
	projectDiagnostics := a.projectDiagnostics(projects, services, metricsDiagnostic, endpoints, recentEvents)
	recordStage("project-diagnostics", projectDiagnosticsStart, StatusOK, fmt.Sprintf("%d project rows", len(projectDiagnostics)))
	eventLog := eventLogDiagnostic(storeDiagnostic.EventRows, recentEvents)
	agentHealthStart := time.Now()
	agentHealth := agentReportDiagnostics(agentReports, metricsDiagnostic)
	recordStage("agent-health-rollup", agentHealthStart, StatusOK, fmt.Sprintf("%d agent health rows", len(agentHealth)))
	if storeDiagnosticErr != nil && eventLog.Total < eventLog.Returned {
		eventLog.Total = eventLog.Returned
	}
	providers := []ProviderDiagnostic{
		{
			Name:      "backend",
			Status:    StatusOK,
			Detail:    "statusd is serving requests",
			CheckedAt: generatedAt,
		},
		{
			Name:      "gatus",
			Status:    providerStatus(gatusErr == nil, len(endpoints) > 0),
			Detail:    providerDetail(gatusErr, fmt.Sprintf("%d endpoints loaded", len(endpoints))),
			LatencyMS: gatusLatency.Milliseconds(),
			CheckedAt: generatedAt,
		},
		{
			Name:      "static-config",
			Status:    configStatus(issues),
			Detail:    fmt.Sprintf("%d issues", len(issues)),
			CheckedAt: generatedAt,
		},
		{
			Name:      "metrics-agents",
			Status:    metricsProviderStatus(metricsErr, metricsDiagnostic),
			Detail:    metricsProviderDetail(metricsErr, metricsDiagnostic),
			CheckedAt: generatedAt,
		},
		{
			Name:      "sqlite",
			Status:    providerStatus(metricsErr == nil && agentReportsErr == nil && recentEventsErr == nil && statusVolatilityErr == nil && storeDiagnosticErr == nil, true),
			Detail:    providerDetail(firstErr(metricsErr, agentReportsErr, recentEventsErr, statusVolatilityErr, storeDiagnosticErr), "store is readable"),
			CheckedAt: generatedAt,
		},
		{
			Name:      "deployment",
			Status:    deployment.Status,
			Detail:    deployment.Detail,
			CheckedAt: generatedAt,
		},
	}

	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Group == endpoints[j].Group {
			return endpoints[i].Name < endpoints[j].Name
		}
		return endpoints[i].Group < endpoints[j].Group
	})

	timing.TotalMS = time.Since(diagnosticsStarted).Milliseconds()

	return &DiagnosticsResponse{
		GeneratedAt: generatedAt,
		Overall:     overall,
		Counts:      counts,
		Providers:   providers,
		Config: ConfigDiagnostic{
			NodeCount:          len(a.config.Nodes),
			ProjectCount:       len(a.config.Projects),
			ServiceCount:       len(a.config.Services),
			GatusEndpointCount: len(endpoints),
			Issues:             issues,
		},
		Metrics:      metricsDiagnostic,
		Runtime:      a.runtimeDiagnostic(generatedAt, storeDiagnostic, timing),
		Deployment:   deployment,
		Projects:     projectDiagnostics,
		Ops:          ops,
		EventLog:     eventLog,
		AgentHealth:  agentHealth,
		AgentReports: agentReports,
		Failures:     failures,
		Checks:       endpoints,
	}, nil
}

func agentReportDiagnostics(reports []MetricsReportLog, metricsDiagnostic MetricsDiagnostic) []AgentNodeDiagnostic {
	nodeIDs := map[string]bool{}
	for _, nodeID := range metricsDiagnostic.ExpectedNodes {
		nodeIDs[nodeID] = true
	}

	reportCount := map[string]int{}
	failedReportCount := map[string]int{}
	collectorFailureCount := map[string]int{}
	latestByNode := map[string]MetricsReportLog{}

	for _, report := range reports {
		nodeIDs[report.NodeID] = true
		reportCount[report.NodeID]++
		collectorFailureCount[report.NodeID] += report.CollectorFailed
		if report.CollectorFailed > 0 {
			failedReportCount[report.NodeID]++
		}
		latest, ok := latestByNode[report.NodeID]
		if !ok || report.ReceivedAt.After(latest.ReceivedAt) || (report.ReceivedAt.Equal(latest.ReceivedAt) && report.ID > latest.ID) {
			latestByNode[report.NodeID] = report
		}
	}

	staleNodes := stringSet(metricsDiagnostic.StaleNodes)
	missingNodes := stringSet(metricsDiagnostic.MissingNodes)
	ids := sortedKeys(nodeIDs)
	diagnostics := make([]AgentNodeDiagnostic, 0, len(ids))
	for _, nodeID := range ids {
		diag := AgentNodeDiagnostic{
			NodeID:                nodeID,
			Status:                StatusOK,
			Detail:                "recent agent reports are clean",
			ReportLagWarnSeconds:  agentReportLagWarnSeconds,
			ReportCount:           reportCount[nodeID],
			FailedReportCount:     failedReportCount[nodeID],
			CollectorFailureCount: collectorFailureCount[nodeID],
		}

		latest, ok := latestByNode[nodeID]
		if !ok {
			diag.Status = StatusUnknown
			if missingNodes[nodeID] {
				diag.Detail = "node has no recent metrics report"
			} else {
				diag.Detail = "node has no report in the recent diagnostics window"
			}
			diagnostics = append(diagnostics, diag)
			continue
		}

		latestReceived := latest.ReceivedAt
		latestCaptured := latest.CapturedAt
		diag.Hostname = latest.Hostname
		diag.LatestReceivedAt = &latestReceived
		diag.LatestCapturedAt = &latestCaptured
		diag.LatestReportLagSeconds = latest.ReportLagSeconds
		diag.ReportLagHeadroomSeconds = agentReportLagWarnSeconds - latest.ReportLagSeconds
		diag.LatestSchemaVersion = latest.SchemaVersion
		diag.LatestCollectorOK = latest.CollectorOK
		diag.LatestCollectorFailed = latest.CollectorFailed
		diag.GPUAvailable = latest.GPUAvailable
		diag.StorageDeviceCount = latest.StorageDeviceCount
		diag.NetworkInterfaceCount = latest.NetworkInterfaceCount
		diag.LatestFailedCollectors = failedCollectors(latest.CollectorStatus)

		details := []string{}
		if staleNodes[nodeID] {
			diag.Status = StatusDegraded
			details = append(details, "latest metrics are stale")
		}
		if latest.ReportLagSeconds > agentReportLagWarnSeconds {
			diag.Status = StatusDegraded
			details = append(details, fmt.Sprintf("latest report lag %.1fs exceeds %ds", latest.ReportLagSeconds, agentReportLagWarnSeconds))
		}
		if latest.CollectorFailed > 0 {
			diag.Status = StatusDegraded
			details = append(details, fmt.Sprintf("latest report has %d collector failures", latest.CollectorFailed))
		} else if diag.FailedReportCount > 0 {
			diag.Status = StatusDegraded
			details = append(details, fmt.Sprintf("%d/%d recent reports had collector failures", diag.FailedReportCount, diag.ReportCount))
		}
		if len(details) > 0 {
			diag.Detail = strings.Join(details, "; ")
		}
		diagnostics = append(diagnostics, diag)
	}

	return diagnostics
}

func failedCollectors(statuses []CollectorStatus) []AgentCollectorFailure {
	failures := []AgentCollectorFailure{}
	for _, status := range statuses {
		if status.OK {
			continue
		}
		failures = append(failures, AgentCollectorFailure{
			Name:            status.Name,
			Detail:          status.Detail,
			ElapsedMS:       status.ElapsedMS,
			Cached:          status.Cached,
			CacheAgeSeconds: status.CacheAgeSeconds,
		})
	}
	return failures
}

func eventLogDiagnostic(total int, events []Event) EventLogDiagnostic {
	diag := EventLogDiagnostic{
		Total:    total,
		Returned: len(events),
		Events:   events,
	}
	if diag.Total < diag.Returned {
		diag.Total = diag.Returned
	}
	if len(events) == 0 {
		return diag
	}

	latest := events[0].CreatedAt
	diag.LatestAt = &latest
	kindCounts := map[string]int{}
	for _, event := range events {
		kindCounts[event.Kind]++
		switch event.To {
		case StatusOK:
			diag.StatusCounts.OK++
		case StatusDegraded:
			diag.StatusCounts.Degraded++
		case StatusDown:
			diag.StatusCounts.Down++
		case StatusMaintenance:
			diag.StatusCounts.Maintenance++
		default:
			diag.StatusCounts.Unknown++
		}
		if event.CreatedAt.After(latest) {
			latest = event.CreatedAt
			diag.LatestAt = &latest
		}
	}

	kinds := make([]string, 0, len(kindCounts))
	for kind := range kindCounts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	diag.KindCounts = make([]EventKindCount, 0, len(kinds))
	for _, kind := range kinds {
		diag.KindCounts = append(diag.KindCounts, EventKindCount{Kind: kind, Count: kindCounts[kind]})
	}
	return diag
}

func (a *Aggregator) projectDiagnostics(projects []ProjectView, services []ServiceView, metricsDiagnostic MetricsDiagnostic, endpoints []RuntimeEndpointStatus, events []Event) []ProjectDiagnostic {
	endpointKeys := map[string]bool{}
	endpointStatusByKey := map[string]RuntimeEndpointStatus{}
	for _, endpoint := range endpoints {
		if endpoint.Key != "" {
			endpointKeys[endpoint.Key] = true
			endpointStatusByKey[endpoint.Key] = endpoint
		}
	}
	reportingNodes := stringSet(metricsDiagnostic.ReportingNodes)
	missingNodes := stringSet(metricsDiagnostic.MissingNodes)
	staleNodes := stringSet(metricsDiagnostic.StaleNodes)

	diagnostics := make([]ProjectDiagnostic, 0, len(projects))
	for _, project := range projects {
		diag := ProjectDiagnostic{
			ProjectID:   project.ID,
			ProjectName: project.Name,
			Status:      project.Status,
			Detail:      "project monitoring coverage is complete",
		}
		nodeSet := map[string]bool{}
		serviceSet := map[string]bool{}
		metricsReporting := map[string]bool{}
		metricsMissing := map[string]bool{}
		metricsStale := map[string]bool{}
		var latestCheck time.Time
		var endpointLatencyTotal int64
		var endpointLatencyCount int

		for _, service := range services {
			if !projectOwnsService(project.ProjectConfig, service) {
				continue
			}
			serviceSet[service.ID] = true
			diag.ServiceCount++
			if service.Critical {
				diag.CriticalServiceCount++
			}
			switch service.Status {
			case StatusDown:
				diag.DownServiceCount++
			case StatusDegraded, StatusUnknown:
				diag.DegradedServiceCount++
			}
			if service.EndpointKey == "" {
				diag.UnmappedServiceCount++
			} else if endpointKeys[service.EndpointKey] {
				diag.EndpointCount++
				endpoint := endpointStatusByKey[service.EndpointKey]
				diag.RecentCheckCount += endpoint.RecentResults
				diag.RecentFailureCount += endpoint.RecentFailures
				if endpoint.RecentResults > endpoint.RecentFailures {
					diag.RecentSuccessCount += endpoint.RecentResults - endpoint.RecentFailures
				}
				if endpoint.RecentResults == 0 {
					diag.NoRecentCheckCount++
				}
				if endpoint.ResponseTimeMS >= 0 {
					endpointLatencyTotal += endpoint.ResponseTimeMS
					endpointLatencyCount++
					if endpoint.ResponseTimeMS > diag.CurrentMaxResponseMS {
						diag.CurrentMaxResponseMS = endpoint.ResponseTimeMS
					}
				}
				if endpoint.LastCheckedAt.After(latestCheck) {
					latestCheck = endpoint.LastCheckedAt
				}
			} else {
				diag.MissingEndpointCount++
			}
			if service.NodeID != "" {
				nodeSet[service.NodeID] = true
				if reportingNodes[service.NodeID] {
					metricsReporting[service.NodeID] = true
				}
				if missingNodes[service.NodeID] {
					metricsMissing[service.NodeID] = true
				}
				if staleNodes[service.NodeID] {
					metricsStale[service.NodeID] = true
				}
			}
		}

		diag.RelatedNodes = sortedKeys(nodeSet)
		if !latestCheck.IsZero() {
			lastCheck := latestCheck
			diag.LastCheckAt = &lastCheck
		}
		if diag.ServiceCount > 0 {
			diag.CheckCoveragePercent = float64(diag.EndpointCount) / float64(diag.ServiceCount) * 100
		}
		if diag.RecentCheckCount > 0 {
			diag.RecentFailurePercent = float64(diag.RecentFailureCount) / float64(diag.RecentCheckCount) * 100
		}
		if endpointLatencyCount > 0 {
			diag.CurrentAvgResponseMS = float64(endpointLatencyTotal) / float64(endpointLatencyCount)
		}
		diag.RecentEventCount, diag.LastEventAt = projectEventSummary(project.ID, serviceSet, nodeSet, events)
		diag.MetricsReportingNodes = sortedKeys(metricsReporting)
		diag.MetricsMissingNodes = sortedKeys(metricsMissing)
		diag.MetricsStaleNodes = sortedKeys(metricsStale)
		details := []string{}
		if diag.ServiceCount == 0 {
			details = append(details, "project has no mapped services")
		}
		if diag.UnmappedServiceCount > 0 {
			details = append(details, fmt.Sprintf("%d services have no endpoint key", diag.UnmappedServiceCount))
		}
		if diag.MissingEndpointCount > 0 {
			details = append(details, fmt.Sprintf("%d endpoint mappings are missing in Gatus", diag.MissingEndpointCount))
		}
		if diag.NoRecentCheckCount > 0 {
			details = append(details, fmt.Sprintf("%d mapped endpoints have no recent check log", diag.NoRecentCheckCount))
		}
		if diag.DownServiceCount > 0 || diag.DegradedServiceCount > 0 {
			details = append(details, fmt.Sprintf("%d down and %d degraded/unknown services", diag.DownServiceCount, diag.DegradedServiceCount))
		}
		if len(diag.MetricsMissingNodes) > 0 {
			details = append(details, fmt.Sprintf("%d related nodes have no metrics", len(diag.MetricsMissingNodes)))
		}
		if len(diag.MetricsStaleNodes) > 0 {
			details = append(details, fmt.Sprintf("%d related nodes have stale metrics", len(diag.MetricsStaleNodes)))
		}
		if len(details) > 0 {
			diag.Status = projectDiagnosticStatus(diag, project.Status)
			diag.Detail = strings.Join(details, "; ")
		}
		diagnostics = append(diagnostics, diag)
	}

	sort.Slice(diagnostics, func(i, j int) bool {
		return diagnostics[i].ProjectID < diagnostics[j].ProjectID
	})
	return diagnostics
}

func projectEventSummary(projectID string, serviceSet map[string]bool, nodeSet map[string]bool, events []Event) (int, *time.Time) {
	count := 0
	var latest time.Time
	for _, event := range events {
		matched := false
		switch event.Kind {
		case "project":
			matched = event.SubjectID == projectID
		case "service":
			matched = serviceSet[event.SubjectID]
		case "node":
			matched = nodeSet[event.SubjectID]
		}
		if !matched {
			continue
		}
		count++
		if event.CreatedAt.After(latest) {
			latest = event.CreatedAt
		}
	}
	if latest.IsZero() {
		return count, nil
	}
	lastEvent := latest
	return count, &lastEvent
}

func projectDiagnosticStatus(diag ProjectDiagnostic, projectStatus Status) Status {
	if diag.DownServiceCount > 0 {
		return StatusDown
	}
	if diag.ServiceCount == 0 || diag.UnmappedServiceCount > 0 || diag.MissingEndpointCount > 0 || diag.NoRecentCheckCount > 0 || len(diag.MetricsMissingNodes) > 0 || len(diag.MetricsStaleNodes) > 0 {
		return StatusDegraded
	}
	if diag.DegradedServiceCount > 0 {
		return StatusDegraded
	}
	return projectStatus
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (a *Aggregator) statusVolatilityDiagnostic(ctx context.Context, now time.Time) (StatusVolatilityDiagnostic, error) {
	diag := StatusVolatilityDiagnostic{
		WindowSeconds:   statusVolatilityWindow.Seconds(),
		ChangeThreshold: statusVolatilityThreshold,
		Subjects:        []StatusVolatilitySubject{},
	}
	if a.store == nil {
		return diag, nil
	}
	subjects, err := a.store.StatusVolatility(ctx, now.Add(-statusVolatilityWindow), statusVolatilityLimit)
	if err != nil {
		return diag, err
	}
	for i := range subjects {
		subject := &subjects[i]
		if subject.ChangeCount >= statusVolatilityThreshold {
			subject.Status = StatusDegraded
		} else {
			subject.Status = StatusOK
		}
		subject.Detail = fmt.Sprintf("%d status changes in the last %.0fh; latest %s -> %s", subject.ChangeCount, statusVolatilityWindow.Hours(), nonEmpty(string(subject.LatestFrom), "-"), nonEmpty(string(subject.LatestTo), "-"))
	}
	diag.Subjects = subjects
	return diag, nil
}

func (a *Aggregator) opsDiagnostic(now time.Time, services []ServiceView, metrics []MetricsView, metricsDiagnostic MetricsDiagnostic, configIssues []ConfigIssue, volatility StatusVolatilityDiagnostic) OpsDiagnostic {
	issues := []OpsIssue{}

	for _, service := range services {
		if service.Status == StatusOK || service.Status == StatusMaintenance {
			continue
		}
		severity := "warn"
		if service.Status == StatusDown && service.Critical {
			severity = "error"
		}
		issues = append(issues, OpsIssue{
			Severity:    severity,
			Kind:        "service-check",
			SubjectID:   service.ID,
			SubjectName: service.Name,
			NodeID:      service.NodeID,
			ProjectID:   service.ProjectID,
			ServiceID:   service.ID,
			Status:      service.Status,
			Metric:      "availability",
			Detail:      service.Detail,
			ObservedAt:  service.LastCheckedAt,
		})
	}

	for _, nodeID := range metricsDiagnostic.MissingNodes {
		issues = append(issues, OpsIssue{
			Severity:   "warn",
			Kind:       "metrics-missing",
			SubjectID:  nodeID,
			NodeID:     nodeID,
			Status:     StatusUnknown,
			Metric:     "agent",
			Detail:     "metrics agent has not reported for this configured node",
			ObservedAt: now,
		})
	}
	for _, nodeID := range metricsDiagnostic.StaleNodes {
		issues = append(issues, OpsIssue{
			Severity:   "warn",
			Kind:       "metrics-stale",
			SubjectID:  nodeID,
			NodeID:     nodeID,
			Status:     StatusDegraded,
			Metric:     "agent",
			Detail:     "latest metrics report is stale",
			ObservedAt: now,
		})
	}
	for _, collector := range metricsDiagnostic.CollectorIssues {
		issues = append(issues, OpsIssue{
			Severity:   "warn",
			Kind:       "collector",
			SubjectID:  collector.NodeID + "/" + collector.Name,
			NodeID:     collector.NodeID,
			Status:     StatusDegraded,
			Metric:     collector.Name,
			Value:      float64(collector.ElapsedMS),
			Unit:       "ms",
			Detail:     nonEmpty(collector.Detail, "collector reported a failure"),
			ObservedAt: now,
		})
	}
	for _, resourceIssue := range metricsDiagnostic.ServiceResourceIssues {
		issues = append(issues, OpsIssue{
			Severity:    resourceIssue.Severity,
			Kind:        "service-resource",
			SubjectID:   resourceIssue.ServiceID,
			SubjectName: resourceIssue.ServiceName,
			NodeID:      resourceIssue.NodeID,
			ServiceID:   resourceIssue.ServiceID,
			Status:      StatusDegraded,
			Metric:      resourceIssue.Metric,
			Value:       resourceIssue.Value,
			Limit:       resourceIssue.Limit,
			Unit:        resourceIssue.Unit,
			Detail:      resourceIssue.Detail,
			ObservedAt:  now,
		})
	}
	for _, configIssue := range configIssues {
		if configIssue.Severity == "info" {
			continue
		}
		issues = append(issues, OpsIssue{
			Severity:   configIssue.Severity,
			Kind:       "config-" + configIssue.Kind,
			SubjectID:  configIssue.SubjectID,
			Status:     statusForIssueSeverity(configIssue.Severity),
			Metric:     "config",
			Detail:     configIssue.Detail,
			ObservedAt: now,
		})
	}
	for _, metric := range metrics {
		issues = append(issues, resourceThresholdIssues(metric, now, a.resourceThresholdsForNode(metric.NodeID))...)
	}
	for _, subject := range volatility.Subjects {
		if subject.ChangeCount < volatility.ChangeThreshold {
			continue
		}
		issue := OpsIssue{
			Severity:    "warn",
			Kind:        "status-volatility",
			SubjectID:   subject.SubjectID,
			SubjectName: subject.Label,
			Status:      subject.Status,
			Metric:      "status_changes",
			Detail:      subject.Detail,
			ObservedAt:  subject.LatestAt,
		}
		switch subject.Kind {
		case "service":
			issue.ServiceID = subject.SubjectID
		case "node":
			issue.NodeID = subject.SubjectID
		case "project":
			issue.ProjectID = subject.SubjectID
		}
		issues = append(issues, issue)
	}

	sort.Slice(issues, func(i, j int) bool {
		if severityRank(issues[i].Severity) == severityRank(issues[j].Severity) {
			if issues[i].NodeID == issues[j].NodeID {
				if issues[i].Kind == issues[j].Kind {
					return issues[i].SubjectID < issues[j].SubjectID
				}
				return issues[i].Kind < issues[j].Kind
			}
			return issues[i].NodeID < issues[j].NodeID
		}
		return severityRank(issues[i].Severity) < severityRank(issues[j].Severity)
	})
	if len(issues) > 50 {
		issues = issues[:50]
	}
	return OpsDiagnostic{
		Issues:             issues,
		Counts:             countOpsIssues(issues),
		ProjectImpacts:     a.projectImpacts(issues),
		StatusVolatility:   volatility,
		ResourceThresholds: a.resourceThresholdViews(metricsDiagnostic.ExpectedNodes),
		ResourceStates:     a.resourceStates(now, metrics),
	}
}

func (a *Aggregator) projectImpacts(issues []OpsIssue) []OpsProjectImpact {
	if len(issues) == 0 {
		return []OpsProjectImpact{}
	}

	projectNames := map[string]string{}
	for _, project := range a.config.Projects {
		projectNames[project.ID] = project.Name
	}

	serviceProjects := map[string]map[string]bool{}
	nodeProjects := map[string]map[string]bool{}
	for _, project := range a.config.Projects {
		for _, serviceID := range project.ServiceIDs {
			addSetValue(serviceProjects, serviceID, project.ID)
		}
	}
	for _, service := range a.config.Services {
		if service.ProjectID != "" {
			addSetValue(serviceProjects, service.ID, service.ProjectID)
		}
		for projectID := range serviceProjects[service.ID] {
			if service.NodeID != "" {
				addSetValue(nodeProjects, service.NodeID, projectID)
			}
		}
	}

	type impactAccumulator struct {
		impact   OpsProjectImpact
		nodes    map[string]bool
		services map[string]bool
		kinds    map[string]bool
	}
	impacts := map[string]*impactAccumulator{}
	ensure := func(projectID string) *impactAccumulator {
		acc := impacts[projectID]
		if acc == nil {
			name := projectNames[projectID]
			if name == "" {
				name = projectID
			}
			acc = &impactAccumulator{
				impact: OpsProjectImpact{
					ProjectID:   projectID,
					ProjectName: name,
					Status:      StatusOK,
				},
				nodes:    map[string]bool{},
				services: map[string]bool{},
				kinds:    map[string]bool{},
			}
			impacts[projectID] = acc
		}
		return acc
	}

	for _, issue := range issues {
		projectIDs := map[string]bool{}
		if issue.ProjectID != "" {
			projectIDs[issue.ProjectID] = true
		}
		if issue.ServiceID != "" {
			for projectID := range serviceProjects[issue.ServiceID] {
				projectIDs[projectID] = true
			}
		}
		if issue.NodeID != "" {
			for projectID := range nodeProjects[issue.NodeID] {
				projectIDs[projectID] = true
			}
		}
		for projectID := range projectIDs {
			acc := ensure(projectID)
			acc.impact.IssueCount++
			switch issue.Severity {
			case "error":
				acc.impact.ErrorCount++
			case "warn":
				acc.impact.WarnCount++
			default:
				acc.impact.InfoCount++
			}
			if issue.NodeID != "" {
				acc.nodes[issue.NodeID] = true
			}
			if issue.ServiceID != "" {
				acc.services[issue.ServiceID] = true
			}
			if issue.Kind != "" {
				acc.kinds[issue.Kind] = true
			}
		}
	}

	rows := make([]OpsProjectImpact, 0, len(impacts))
	for _, acc := range impacts {
		row := acc.impact
		row.AffectedNodes = sortedKeys(acc.nodes)
		row.AffectedServices = sortedKeys(acc.services)
		row.IssueKinds = sortedKeys(acc.kinds)
		row.Status = StatusOK
		if row.ErrorCount > 0 {
			row.Status = StatusDown
		} else if row.WarnCount > 0 || row.InfoCount > 0 {
			row.Status = StatusDegraded
		}
		row.Detail = fmt.Sprintf("%d issues across %d kinds", row.IssueCount, len(row.IssueKinds))
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].ErrorCount == rows[j].ErrorCount {
			if rows[i].WarnCount == rows[j].WarnCount {
				if rows[i].IssueCount == rows[j].IssueCount {
					return rows[i].ProjectID < rows[j].ProjectID
				}
				return rows[i].IssueCount > rows[j].IssueCount
			}
			return rows[i].WarnCount > rows[j].WarnCount
		}
		return rows[i].ErrorCount > rows[j].ErrorCount
	})
	if len(rows) > 20 {
		rows = rows[:20]
	}
	return rows
}

func addSetValue(values map[string]map[string]bool, key string, value string) {
	if key == "" || value == "" {
		return
	}
	set := values[key]
	if set == nil {
		set = map[string]bool{}
		values[key] = set
	}
	set[value] = true
}

func resourceThresholdIssues(metric MetricsView, now time.Time, thresholds EffectiveResourceThreshold) []OpsIssue {
	thresholdRules := []struct {
		kind   string
		metric string
		value  float64
		limit  float64
		unit   string
		detail string
	}{
		{kind: "resource-cpu", metric: "cpu", value: metric.CPU.Percent, limit: thresholds.CPUPercent, unit: "%", detail: "node CPU is above threshold"},
		{kind: "resource-memory", metric: "memory", value: metric.Memory.Percent, limit: thresholds.MemoryPercent, unit: "%", detail: "node memory is above threshold"},
		{kind: "resource-disk", metric: "disk", value: metric.Disk.Percent, limit: thresholds.RootDiskPercent, unit: "%", detail: "root disk usage is above threshold"},
		{kind: "resource-network-rx", metric: "network_rx", value: metric.Network.RXBps, limit: thresholds.NetworkRXBps, unit: "B/s", detail: "network receive rate is above threshold"},
		{kind: "resource-network-tx", metric: "network_tx", value: metric.Network.TXBps, limit: thresholds.NetworkTXBps, unit: "B/s", detail: "network transmit rate is above threshold"},
		{kind: "resource-storage-read", metric: "storage_read", value: metric.Storage.ReadBps, limit: thresholds.StorageReadBps, unit: "B/s", detail: "storage read rate is above threshold"},
		{kind: "resource-storage-write", metric: "storage_write", value: metric.Storage.WriteBps, limit: thresholds.StorageWriteBps, unit: "B/s", detail: "storage write rate is above threshold"},
	}
	issues := []OpsIssue{}
	for _, threshold := range thresholdRules {
		if threshold.limit <= 0 {
			continue
		}
		if threshold.value < threshold.limit {
			continue
		}
		issues = append(issues, OpsIssue{
			Severity:   "warn",
			Kind:       threshold.kind,
			SubjectID:  metric.NodeID,
			NodeID:     metric.NodeID,
			Status:     StatusDegraded,
			Metric:     threshold.metric,
			Value:      threshold.value,
			Limit:      threshold.limit,
			Unit:       threshold.unit,
			Detail:     threshold.detail,
			ObservedAt: newestTime(metric.UpdatedAt, now),
		})
	}
	if metric.GPU.Available {
		for _, gpu := range metric.GPU.Devices {
			if gpu.UtilPercent < thresholds.GPUUtilPercent {
				continue
			}
			issues = append(issues, OpsIssue{
				Severity:    "warn",
				Kind:        "resource-gpu",
				SubjectID:   metric.NodeID,
				SubjectName: gpu.Name,
				NodeID:      metric.NodeID,
				Status:      StatusDegraded,
				Metric:      "gpu",
				Value:       gpu.UtilPercent,
				Limit:       thresholds.GPUUtilPercent,
				Unit:        "%",
				Detail:      "GPU utilization is above threshold",
				ObservedAt:  newestTime(metric.UpdatedAt, now),
			})
		}
	}
	return issues
}

func (a *Aggregator) resourceStates(now time.Time, metrics []MetricsView) []OpsResourceState {
	states := make([]OpsResourceState, 0, len(metrics))
	for _, metric := range metrics {
		thresholds := a.resourceThresholdsForNode(metric.NodeID)
		gpuName, gpuUtil := maxGPUUtil(metric.GPU)
		state := OpsResourceState{
			NodeID:       metric.NodeID,
			Status:       StatusOK,
			Detail:       "within configured resource thresholds",
			ObservedAt:   newestTime(metric.UpdatedAt, now),
			Stale:        metric.Stale,
			CPU:          resourceHeadroom(metric.CPU.Percent, thresholds.CPUPercent, "%"),
			Memory:       resourceHeadroom(metric.Memory.Percent, thresholds.MemoryPercent, "%"),
			RootDisk:     resourceHeadroom(metric.Disk.Percent, thresholds.RootDiskPercent, "%"),
			GPUAvailable: metric.GPU.Available,
			GPUName:      gpuName,
			GPU:          resourceHeadroom(gpuUtil, gpuThresholdLimit(metric.GPU, thresholds.GPUUtilPercent), "%"),
			NetworkRX:    resourceHeadroom(metric.Network.RXBps, thresholds.NetworkRXBps, "B/s"),
			NetworkTX:    resourceHeadroom(metric.Network.TXBps, thresholds.NetworkTXBps, "B/s"),
			StorageRead:  resourceHeadroom(metric.Storage.ReadBps, thresholds.StorageReadBps, "B/s"),
			StorageWrite: resourceHeadroom(metric.Storage.WriteBps, thresholds.StorageWriteBps, "B/s"),
		}
		details := []string{}
		if metric.Stale {
			state.Status = StatusDegraded
			details = append(details, "latest metrics report is stale")
		}
		markHeadroomIssue(&state, &details, "cpu", state.CPU)
		markHeadroomIssue(&state, &details, "memory", state.Memory)
		markHeadroomIssue(&state, &details, "root disk", state.RootDisk)
		markHeadroomIssue(&state, &details, "gpu", state.GPU)
		markHeadroomIssue(&state, &details, "network rx", state.NetworkRX)
		markHeadroomIssue(&state, &details, "network tx", state.NetworkTX)
		markHeadroomIssue(&state, &details, "storage read", state.StorageRead)
		markHeadroomIssue(&state, &details, "storage write", state.StorageWrite)
		if len(details) > 0 {
			state.Detail = strings.Join(details, "; ")
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].NodeID < states[j].NodeID
	})
	return states
}

func resourceHeadroom(value float64, limit float64, unit string) ResourceHeadroom {
	state := ResourceHeadroom{
		Configured: limit > 0,
		Value:      value,
		Limit:      limit,
		Unit:       unit,
	}
	if state.Configured {
		state.Headroom = limit - value
	}
	return state
}

func markHeadroomIssue(state *OpsResourceState, details *[]string, label string, headroom ResourceHeadroom) {
	if !headroom.Configured || headroom.Value < headroom.Limit {
		return
	}
	state.Status = StatusDegraded
	*details = append(*details, label+" over threshold")
}

func gpuThresholdLimit(gpu GPUMetrics, limit float64) float64 {
	if !gpu.Available {
		return 0
	}
	return limit
}

func maxGPUUtil(gpu GPUMetrics) (string, float64) {
	if !gpu.Available {
		return "", 0
	}
	var selectedName string
	var selectedUtil float64
	for i, device := range gpu.Devices {
		name := nonEmpty(device.Name, device.Index)
		if i == 0 || device.UtilPercent > selectedUtil {
			selectedName = name
			selectedUtil = device.UtilPercent
		}
	}
	return selectedName, selectedUtil
}

func (a *Aggregator) resourceThresholdsForNode(nodeID string) EffectiveResourceThreshold {
	if a == nil || a.config == nil {
		return defaultResourceThreshold(nodeID)
	}
	return a.config.Diagnostics.ResourceThresholds.EffectiveForNode(nodeID)
}

func (a *Aggregator) resourceThresholdViews(nodeIDs []string) []EffectiveResourceThreshold {
	if len(nodeIDs) == 0 && a != nil && a.config != nil {
		for _, node := range a.config.Nodes {
			nodeIDs = append(nodeIDs, node.ID)
		}
	}
	seen := map[string]bool{}
	views := make([]EffectiveResourceThreshold, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if nodeID == "" || seen[nodeID] {
			continue
		}
		seen[nodeID] = true
		views = append(views, a.resourceThresholdsForNode(nodeID))
	}
	sort.Slice(views, func(i, j int) bool {
		return views[i].NodeID < views[j].NodeID
	})
	return views
}

func (c ResourceThresholdConfig) EffectiveForNode(nodeID string) EffectiveResourceThreshold {
	threshold := defaultResourceThreshold(nodeID)
	applyResourceThresholdConfig(&threshold, c)
	if c.Nodes != nil {
		if override, ok := c.Nodes[nodeID]; ok {
			applyResourceThresholdOverride(&threshold, override)
		}
	}
	return threshold
}

func defaultResourceThreshold(nodeID string) EffectiveResourceThreshold {
	return EffectiveResourceThreshold{
		NodeID:          nodeID,
		CPUPercent:      defaultCPUThresholdPercent,
		MemoryPercent:   defaultMemoryThresholdPercent,
		RootDiskPercent: defaultRootDiskThresholdPercent,
		GPUUtilPercent:  defaultGPUUtilThresholdPercent,
	}
}

func applyResourceThresholdConfig(threshold *EffectiveResourceThreshold, config ResourceThresholdConfig) {
	applyFloatPointer(&threshold.CPUPercent, config.CPUPercent)
	applyFloatPointer(&threshold.MemoryPercent, config.MemoryPercent)
	applyFloatPointer(&threshold.RootDiskPercent, config.RootDiskPercent)
	applyFloatPointer(&threshold.GPUUtilPercent, config.GPUUtilPercent)
	applyFloatPointer(&threshold.NetworkRXBps, config.NetworkRXBps)
	applyFloatPointer(&threshold.NetworkTXBps, config.NetworkTXBps)
	applyFloatPointer(&threshold.StorageReadBps, config.StorageReadBps)
	applyFloatPointer(&threshold.StorageWriteBps, config.StorageWriteBps)
}

func applyResourceThresholdOverride(threshold *EffectiveResourceThreshold, override ResourceThresholdOverride) {
	applyFloatPointer(&threshold.CPUPercent, override.CPUPercent)
	applyFloatPointer(&threshold.MemoryPercent, override.MemoryPercent)
	applyFloatPointer(&threshold.RootDiskPercent, override.RootDiskPercent)
	applyFloatPointer(&threshold.GPUUtilPercent, override.GPUUtilPercent)
	applyFloatPointer(&threshold.NetworkRXBps, override.NetworkRXBps)
	applyFloatPointer(&threshold.NetworkTXBps, override.NetworkTXBps)
	applyFloatPointer(&threshold.StorageReadBps, override.StorageReadBps)
	applyFloatPointer(&threshold.StorageWriteBps, override.StorageWriteBps)
}

func applyFloatPointer(target *float64, value *float64) {
	if value != nil {
		*target = *value
	}
}

func statusForIssueSeverity(severity string) Status {
	if severity == "error" {
		return StatusDown
	}
	if severity == "warn" {
		return StatusDegraded
	}
	return StatusUnknown
}

func countOpsIssues(issues []OpsIssue) OpsIssueCounts {
	var counts OpsIssueCounts
	for _, issue := range issues {
		switch issue.Severity {
		case "error":
			counts.Error++
		case "warn":
			counts.Warn++
		default:
			counts.Info++
		}
	}
	return counts
}

func newestTime(value time.Time, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

func nonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func (a *Aggregator) storeDiagnostic(ctx context.Context) (StoreDiagnostic, error) {
	if a.store == nil {
		return StoreDiagnostic{}, errors.New("store is not configured")
	}
	return a.store.Diagnostics(ctx)
}

func (a *Aggregator) runtimeDiagnostic(now time.Time, store StoreDiagnostic, diagnosticsTiming RuntimeTimingDiagnostic) RuntimeDiagnostic {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return RuntimeDiagnostic{
		UptimeSeconds: now.Sub(a.startedAt).Seconds(),
		GoVersion:     runtime.Version(),
		Goroutines:    runtime.NumGoroutine(),
		Build: RuntimeBuildDiagnostic{
			Commit:  strings.TrimSpace(a.runtime.BuildCommit),
			BuiltAt: strings.TrimSpace(a.runtime.BuildTime),
		},
		Diagnostics: diagnosticsTiming,
		Memory: RuntimeMemoryDiagnostic{
			AllocBytes:     mem.Alloc,
			SysBytes:       mem.Sys,
			HeapAllocBytes: mem.HeapAlloc,
			HeapInuseBytes: mem.HeapInuse,
			NumGC:          mem.NumGC,
			LastGCPauseMS:  float64(mem.PauseNs[(mem.NumGC+255)%256]) / 1e6,
		},
		SummaryCache: a.cacheDiagnostic(now),
		Store:        store,
	}
}

func (a *Aggregator) deploymentDiagnostic(store StoreDiagnostic) DeploymentDiagnostic {
	settings := a.runtime
	mode := strings.TrimSpace(settings.DeploymentMode)
	if mode == "" {
		mode = "development"
	}

	checks := []DeploymentCheck{}
	add := func(key, category string, status Status, expected, actual, detail string) {
		checks = append(checks, DeploymentCheck{
			Key:      key,
			Category: category,
			Status:   status,
			Expected: expected,
			Actual:   actual,
			Detail:   detail,
		})
	}
	addEqual := func(key, category, expected, actual, detail string) {
		status := StatusOK
		if strings.TrimSpace(actual) != expected {
			status = StatusDegraded
		}
		add(key, category, status, expected, actual, detail)
	}

	add("deployment-mode", "runtime", StatusOK, "", mode, "runtime deployment mode")
	if mode == "production" {
		addEqual("listen-addr", "network", "127.0.0.1:23180", settings.ListenAddr, "statusd must stay bound to localhost on the reserved production port")
		addEqual("gatus-url", "network", "http://127.0.0.1:23181", normalizeRuntimeURL(settings.GatusURL), "statusd should read Gatus through the localhost production port")
		addEqual("config-path", "filesystem", "/app/config/status-board.yaml", filepath.Clean(settings.ConfigPath), "production config should come from the runtime image config path")
		addEqual("db-path", "filesystem", "/data/status-board.db", filepath.Clean(settings.DBPath), "SQLite should live in the production data volume")
		addEqual("frontend-dir", "filesystem", "/app/frontend", filepath.Clean(settings.FrontendDir), "static frontend should be served from the runtime image")
		tailnetURL := normalizeRuntimeURL(settings.TailnetStatusURL)
		tailnetStatus := StatusOK
		tailnetDetail := "Tailnet or private status-board entry used by agents and operators"
		if tailnetURL == "" {
			tailnetStatus = StatusDegraded
			tailnetDetail = "Tailnet or private status-board entry is not configured"
		} else if !strings.HasPrefix(tailnetURL, "http://") && !strings.HasPrefix(tailnetURL, "https://") {
			tailnetStatus = StatusDegraded
			tailnetDetail = "Tailnet or private status-board entry must be an HTTP(S) URL"
		}
		add("tailnet-url", "network", tailnetStatus, "configured HTTP(S) Tailnet/private URL", tailnetURL, tailnetDetail)
		checks = append(checks, publicIPEntryCheck(settings.PublicIP))
		checks = append(checks, publicDomainDNSCheck(settings.PublicDomain, settings.PublicIP, net.DefaultResolver.LookupHost))
		if publicEntryReadyForLiveCheck(settings.PublicDomain, settings.PublicIP) {
			client := &http.Client{Timeout: 1500 * time.Millisecond}
			checks = append(checks, endpointHealthCheck("tailnet-health", "network", tailnetURL, http.StatusOK, "Tailnet/private Nginx entry returns health without public credentials", client))
			checks = append(checks, endpointHealthCheck("public-http-auth", "network", publicEntryURL("http", settings.PublicDomain), http.StatusUnauthorized, "public HTTP entry is protected by Basic Auth", client))
			checks = append(checks, endpointHealthCheck("public-https-auth", "network", publicEntryURL("https", settings.PublicDomain), http.StatusUnauthorized, "public HTTPS entry has a valid certificate and is protected by Basic Auth", client))
		} else {
			add("public-http-auth", "network", StatusDegraded, "HTTP 401 from public domain", "", "public HTTP live check skipped because public domain or IP is not configured")
			add("public-https-auth", "network", StatusDegraded, "HTTPS 401 from public domain", "", "public HTTPS live check skipped because public domain or IP is not configured")
		}
	} else {
		add("production-boundary", "runtime", StatusOK, "production", mode, "production-only boundary checks are skipped outside production mode")
	}

	if settings.FrontendDir == "" {
		add("frontend-index", "filesystem", StatusDegraded, "index.html", "", "frontend directory is not configured")
	} else if _, err := os.Stat(filepath.Join(settings.FrontendDir, "index.html")); err != nil {
		add("frontend-index", "filesystem", StatusDegraded, "index.html", filepath.Join(settings.FrontendDir, "index.html"), "frontend index is not readable")
	} else {
		add("frontend-index", "filesystem", StatusOK, "index.html", filepath.Join(settings.FrontendDir, "index.html"), "frontend index is readable")
	}

	if store.TotalSizeBytes > 512*1024*1024 {
		add("sqlite-size", "storage", StatusDegraded, "<=512MiB", fmt.Sprintf("%d", store.TotalSizeBytes), "SQLite files are above the lightweight deployment budget")
	} else {
		add("sqlite-size", "storage", StatusOK, "<=512MiB", fmt.Sprintf("%d", store.TotalSizeBytes), "SQLite files are within the lightweight deployment budget")
	}

	if settings.CacheTTL <= 0 {
		add("summary-cache-ttl", "runtime", StatusDegraded, ">0s", settings.CacheTTL.String(), "summary cache TTL must be positive")
	} else if mode == "production" && settings.CacheTTL > 30*time.Second {
		add("summary-cache-ttl", "runtime", StatusDegraded, "<=30s", settings.CacheTTL.String(), "production cache TTL is unexpectedly high")
	} else {
		add("summary-cache-ttl", "runtime", StatusOK, "0s < ttl <=30s in production", settings.CacheTTL.String(), "summary cache TTL is within bounds")
	}

	retentionDays := settings.MetricsRetention.Hours() / 24
	if settings.MetricsRetention <= 0 {
		add("metrics-retention", "storage", StatusDegraded, ">0d", settings.MetricsRetention.String(), "metrics retention must be positive")
	} else if mode == "production" && retentionDays > 45 {
		add("metrics-retention", "storage", StatusDegraded, "<=45d", fmt.Sprintf("%.1fd", retentionDays), "production retention is above the lightweight default envelope")
	} else {
		add("metrics-retention", "storage", StatusOK, "<=45d in production", fmt.Sprintf("%.1fd", retentionDays), "metrics retention is within bounds")
	}

	sort.Slice(checks, func(i, j int) bool {
		if checks[i].Category == checks[j].Category {
			return checks[i].Key < checks[j].Key
		}
		return checks[i].Category < checks[j].Category
	})

	status := StatusOK
	failing := 0
	for _, check := range checks {
		if check.Status != StatusOK {
			status = StatusDegraded
			failing++
		}
	}
	detail := "deployment boundary checks passed"
	if failing > 0 {
		detail = fmt.Sprintf("%d deployment boundary checks need attention", failing)
	}
	return DeploymentDiagnostic{
		Status: status,
		Mode:   mode,
		Detail: detail,
		Checks: checks,
	}
}

func publicIPEntryCheck(publicIP string) DeploymentCheck {
	publicIP = strings.TrimSpace(publicIP)
	check := DeploymentCheck{
		Key:      "public-ip-entry",
		Category: "network",
		Status:   StatusOK,
		Expected: "valid public IP with /status-board/ protected by Basic Auth",
		Actual:   publicIP,
		Detail:   "public IP entry is configured; verify-sh-core checks that unauthenticated access returns 401",
	}
	if publicIP == "" {
		check.Status = StatusDegraded
		check.Detail = "expected public IP is not configured"
		return check
	}
	if net.ParseIP(publicIP) == nil {
		check.Status = StatusDegraded
		check.Detail = "expected public IP is not a valid IP address"
	}
	return check
}

type deploymentHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func endpointHealthCheck(key string, category string, baseURL string, expectedStatus int, okDetail string, doer deploymentHTTPDoer) DeploymentCheck {
	baseURL = normalizeRuntimeURL(baseURL)
	healthURL := deploymentHealthURL(baseURL)
	check := DeploymentCheck{
		Key:      key,
		Category: category,
		Status:   StatusOK,
		Expected: fmt.Sprintf("HTTP %d from %s", expectedStatus, nonEmpty(healthURL, "configured HTTP(S) URL")),
		Actual:   baseURL,
		Detail:   okDetail,
	}
	if baseURL == "" {
		check.Status = StatusDegraded
		check.Detail = "entry URL is not configured"
		return check
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		check.Status = StatusDegraded
		check.Detail = "entry URL must use HTTP or HTTPS"
		return check
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, healthURL, nil)
	if err != nil {
		check.Status = StatusDegraded
		check.Actual = err.Error()
		check.Detail = "entry health request could not be built"
		return check
	}
	resp, err := doer.Do(req)
	if err != nil {
		check.Status = StatusDegraded
		check.Actual = err.Error()
		check.Detail = "entry health request failed from the statusd runtime"
		return check
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	check.Actual = fmt.Sprintf("HTTP %d", resp.StatusCode)
	if resp.StatusCode != expectedStatus {
		check.Status = StatusDegraded
		check.Detail = fmt.Sprintf("entry health returned HTTP %d, want %d", resp.StatusCode, expectedStatus)
	}
	return check
}

func deploymentHealthURL(baseURL string) string {
	baseURL = normalizeRuntimeURL(baseURL)
	if baseURL == "" {
		return ""
	}
	return baseURL + "/api/v1/health"
}

func publicEntryReadyForLiveCheck(domain string, publicIP string) bool {
	domain = strings.TrimSpace(domain)
	publicIP = strings.TrimSpace(publicIP)
	return domain != "" && domain != "status.example.com" && publicIP != "" && publicIP != "203.0.113.10"
}

func publicEntryURL(scheme string, domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	return scheme + "://" + domain
}

func publicDomainDNSCheck(domain string, expectedIP string, lookup func(context.Context, string) ([]string, error)) DeploymentCheck {
	domain = strings.TrimSpace(domain)
	expectedIP = strings.TrimSpace(expectedIP)
	check := DeploymentCheck{
		Key:      "public-domain-dns",
		Category: "network",
		Status:   StatusOK,
		Expected: expectedIP,
		Actual:   domain,
		Detail:   "public domain resolves to the expected public IP",
	}
	if domain == "" || expectedIP == "" {
		check.Status = StatusDegraded
		check.Detail = "public domain or expected public IP is not configured"
		return check
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	ips, err := lookup(ctx, domain)
	if err != nil {
		check.Status = StatusDegraded
		check.Actual = err.Error()
		check.Detail = "public domain DNS lookup failed from the statusd runtime"
		return check
	}
	unique := uniqueSortedStrings(ips)
	check.Actual = strings.Join(unique, ", ")
	for _, ip := range unique {
		if ip == expectedIP {
			return check
		}
	}
	check.Status = StatusDegraded
	if containsFakeIP(unique) {
		check.Detail = "public domain resolved to a local proxy fake-IP range instead of the expected public IP"
	} else {
		check.Detail = "public domain does not resolve to the expected public IP"
	}
	return check
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = true
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsFakeIP(values []string) bool {
	for _, value := range values {
		ip := net.ParseIP(value).To4()
		if ip == nil {
			continue
		}
		if ip[0] == 198 && (ip[1] == 18 || ip[1] == 19) {
			return true
		}
	}
	return false
}

func normalizeRuntimeURL(value string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	if trimmed == "" {
		return ""
	}
	return trimmed
}

func (a *Aggregator) cacheDiagnostic(now time.Time) SummaryCacheDiagnostic {
	a.mu.Lock()
	defer a.mu.Unlock()

	cached := a.cached != nil && now.Before(a.expiresAt)
	secondsUntilExpiry := a.expiresAt.Sub(now).Seconds()
	if secondsUntilExpiry < 0 {
		secondsUntilExpiry = 0
	}
	var expiresAt *time.Time
	if !a.expiresAt.IsZero() {
		value := a.expiresAt.UTC()
		expiresAt = &value
	}
	return SummaryCacheDiagnostic{
		TTLSeconds:         a.ttl.Seconds(),
		Cached:             cached,
		ExpiresAt:          expiresAt,
		SecondsUntilExpiry: secondsUntilExpiry,
	}
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func projectDetailFromSummary(summary *SummaryResponse, projectID string) (*ProjectDetailResponse, error) {
	project, err := projectFromSummary(summary, projectID)
	if err != nil {
		return nil, err
	}

	serviceIDs := map[string]bool{}
	nodeIDs := map[string]bool{}
	services := []ServiceView{}
	failures := []ServiceView{}
	for _, service := range summary.Services {
		if !projectOwnsService(project.ProjectConfig, service) {
			continue
		}
		serviceIDs[service.ID] = true
		nodeIDs[service.NodeID] = true
		services = append(services, service)
		if service.Status != StatusOK && service.Status != StatusMaintenance {
			failures = append(failures, service)
		}
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].NodeID == services[j].NodeID {
			return services[i].Name < services[j].Name
		}
		return services[i].NodeID < services[j].NodeID
	})

	nodes := []NodeView{}
	for _, node := range summary.Nodes {
		if nodeIDs[node.ID] {
			nodes = append(nodes, node)
		}
	}
	metrics := []MetricsView{}
	for _, metric := range summary.Metrics {
		if nodeIDs[metric.NodeID] {
			metrics = append(metrics, metric)
		}
	}
	events := []Event{}
	for _, event := range summary.Events {
		if event.Kind == "project" && event.SubjectID == projectID {
			events = append(events, event)
			continue
		}
		if event.Kind == "service" && serviceIDs[event.SubjectID] {
			events = append(events, event)
			continue
		}
		if event.Kind == "node" && nodeIDs[event.SubjectID] {
			events = append(events, event)
		}
	}

	return &ProjectDetailResponse{
		GeneratedAt: summary.GeneratedAt,
		Project:     project,
		Nodes:       nodes,
		Services:    services,
		Metrics:     metrics,
		Events:      events,
		Failures:    failures,
	}, nil
}

func projectFromSummary(summary *SummaryResponse, projectID string) (ProjectView, error) {
	for _, item := range summary.Projects {
		if item.ID == projectID {
			return item, nil
		}
	}
	return ProjectView{}, fmt.Errorf("project %s not found", projectID)
}

func projectNodeIDsFromSummary(summary *SummaryResponse, projectID string) []string {
	project, err := projectFromSummary(summary, projectID)
	if err != nil {
		return []string{}
	}
	nodeIDs := map[string]bool{}
	for _, service := range summary.Services {
		if projectOwnsService(project.ProjectConfig, service) && service.NodeID != "" {
			nodeIDs[service.NodeID] = true
		}
	}
	ids := make([]string, 0, len(nodeIDs))
	for nodeID := range nodeIDs {
		ids = append(ids, nodeID)
	}
	sort.Strings(ids)
	return ids
}

func projectServicesFromSummary(summary *SummaryResponse, project ProjectConfig) []ServiceView {
	services := []ServiceView{}
	for _, service := range summary.Services {
		if projectOwnsService(project, service) {
			services = append(services, service)
		}
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].NodeID == services[j].NodeID {
			return services[i].Name < services[j].Name
		}
		return services[i].NodeID < services[j].NodeID
	})
	return services
}

func projectOwnsService(project ProjectConfig, service ServiceView) bool {
	if service.ProjectID == project.ID {
		return true
	}
	for _, serviceID := range project.ServiceIDs {
		if service.ID == serviceID {
			return true
		}
	}
	return false
}

func summarizeMetricsHistory(points []MetricsHistoryPoint) MetricsHistorySummary {
	summary := MetricsHistorySummary{Samples: len(points)}
	for _, point := range points {
		summary.MaxCPUPercent = maxFloat(summary.MaxCPUPercent, point.CPUPercent)
		summary.MaxMemoryPercent = maxFloat(summary.MaxMemoryPercent, point.MemoryPercent)
		summary.MaxDiskPercent = maxFloat(summary.MaxDiskPercent, point.DiskPercent)
		summary.MaxNetworkRXBps = maxFloat(summary.MaxNetworkRXBps, point.NetworkRXBps)
		summary.MaxNetworkTXBps = maxFloat(summary.MaxNetworkTXBps, point.NetworkTXBps)
		summary.MaxStorageReadBps = maxFloat(summary.MaxStorageReadBps, point.StorageReadBps)
		summary.MaxStorageWriteBps = maxFloat(summary.MaxStorageWriteBps, point.StorageWriteBps)
		summary.MaxStorageReadIOPS = maxFloat(summary.MaxStorageReadIOPS, point.StorageReadIOPS)
		summary.MaxStorageWriteIOPS = maxFloat(summary.MaxStorageWriteIOPS, point.StorageWriteIOPS)
		if point.GPUAvailable {
			summary.GPUAvailable = true
			summary.MaxGPUPercent = maxFloat(summary.MaxGPUPercent, point.GPUPercent)
		}
	}
	return summary
}

func maxFloat(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

func nodeFromSummary(summary *SummaryResponse, nodeID string) (NodeView, error) {
	for _, item := range summary.Nodes {
		if item.ID == nodeID {
			return item, nil
		}
	}
	return NodeView{}, fmt.Errorf("node %s not found", nodeID)
}

func nodeDetailFromSummary(summary *SummaryResponse, nodeID string) (*NodeDetailResponse, error) {
	node, err := nodeFromSummary(summary, nodeID)
	if err != nil {
		return nil, err
	}

	serviceIDs := map[string]bool{}
	projectIDs := map[string]bool{}
	services := []ServiceView{}
	failures := []ServiceView{}
	for _, service := range summary.Services {
		if service.NodeID != nodeID {
			continue
		}
		serviceIDs[service.ID] = true
		projectIDs[service.ProjectID] = true
		services = append(services, service)
		if service.Status != StatusOK && service.Status != StatusMaintenance {
			failures = append(failures, service)
		}
	}

	projects := []ProjectView{}
	for _, project := range summary.Projects {
		if projectIDs[project.ID] {
			projects = append(projects, project)
		}
	}

	var metrics *MetricsView
	for _, item := range summary.Metrics {
		if item.NodeID == nodeID {
			copy := item
			metrics = &copy
			break
		}
	}

	events := []Event{}
	for _, event := range summary.Events {
		if event.Kind == "node" && event.SubjectID == nodeID {
			events = append(events, event)
			continue
		}
		if event.Kind == "service" && serviceIDs[event.SubjectID] {
			events = append(events, event)
			continue
		}
		if event.Kind == "project" && projectIDs[event.SubjectID] {
			events = append(events, event)
		}
	}

	return &NodeDetailResponse{
		GeneratedAt: summary.GeneratedAt,
		Node:        node,
		Services:    services,
		Projects:    projects,
		Metrics:     metrics,
		Events:      events,
		Failures:    failures,
	}, nil
}

func serviceDetailFromSummary(summary *SummaryResponse, serviceID string) (*ServiceDetailResponse, error) {
	service, err := serviceFromSummary(summary, serviceID)
	if err != nil {
		return nil, err
	}

	var node *NodeView
	for _, item := range summary.Nodes {
		if item.ID == service.NodeID {
			copy := item
			node = &copy
			break
		}
	}
	var project *ProjectView
	for _, item := range summary.Projects {
		if item.ID == service.ProjectID {
			copy := item
			project = &copy
			break
		}
	}
	var metrics *MetricsView
	for _, item := range summary.Metrics {
		if item.NodeID == service.NodeID {
			copy := item
			metrics = &copy
			break
		}
	}

	var latestCheck *RuntimeCheck
	if service.EndpointKey != "" {
		latestCheck = &RuntimeCheck{
			Key:            service.EndpointKey,
			Status:         service.Status,
			Success:        service.Status == StatusOK || service.Status == StatusMaintenance,
			Detail:         service.Detail,
			ResponseTimeMS: service.ResponseTimeMS,
			LastCheckedAt:  service.LastCheckedAt,
		}
	}

	events := []Event{}
	for _, event := range summary.Events {
		if event.Kind == "service" && event.SubjectID == service.ID {
			events = append(events, event)
			continue
		}
		if event.Kind == "node" && event.SubjectID == service.NodeID {
			events = append(events, event)
			continue
		}
		if event.Kind == "project" && event.SubjectID == service.ProjectID {
			events = append(events, event)
		}
	}

	return &ServiceDetailResponse{
		GeneratedAt: summary.GeneratedAt,
		Service:     service,
		Node:        node,
		Project:     project,
		Metrics:     metrics,
		LatestCheck: latestCheck,
		Events:      events,
	}, nil
}

func serviceFromSummary(summary *SummaryResponse, serviceID string) (ServiceView, error) {
	for _, item := range summary.Services {
		if item.ID == serviceID {
			return item, nil
		}
	}
	return ServiceView{}, fmt.Errorf("service %s not found", serviceID)
}

func (a *Aggregator) serviceViews(checks map[string]RuntimeCheck, gatusErr error) []ServiceView {
	services := make([]ServiceView, 0, len(a.config.Services))
	for _, service := range a.config.Services {
		view := ServiceView{
			ServiceConfig: service,
			Status:        StatusUnknown,
			Detail:        "No endpoint configured",
		}
		if gatusErr != nil {
			view.Detail = fmt.Sprintf("Gatus unavailable: %v", gatusErr)
		}
		if service.EndpointKey != "" {
			if check, ok := checks[service.EndpointKey]; ok {
				view.Status = check.Status
				view.Detail = check.Detail
				view.ResponseTimeMS = check.ResponseTimeMS
				view.LastCheckedAt = check.LastCheckedAt
			} else if gatusErr == nil {
				view.Detail = "Endpoint not found in Gatus"
			}
		}
		services = append(services, view)
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].NodeID == services[j].NodeID {
			return services[i].Name < services[j].Name
		}
		return services[i].NodeID < services[j].NodeID
	})
	return services
}

func (a *Aggregator) configIssues(endpoints []RuntimeEndpointStatus, gatusErr error) []ConfigIssue {
	issues := []ConfigIssue{}
	nodeIDs := map[string]bool{}
	projectIDs := map[string]bool{}
	serviceIDs := map[string]bool{}
	serviceEndpointKeys := map[string]bool{}
	endpointKeys := map[string]bool{}
	projectServiceRefs := map[string]map[string]bool{}

	for _, node := range a.config.Nodes {
		nodeIDs[node.ID] = true
	}
	for _, project := range a.config.Projects {
		projectIDs[project.ID] = true
	}
	for _, endpoint := range endpoints {
		endpointKeys[endpoint.Key] = true
	}
	for _, service := range a.config.Services {
		serviceIDs[service.ID] = true
		if service.NodeID != "" && !nodeIDs[service.NodeID] {
			issues = append(issues, ConfigIssue{Severity: "error", Kind: "service-node", SubjectID: service.ID, Detail: "service references an unknown node"})
		}
		if service.ProjectID != "" && !projectIDs[service.ProjectID] {
			issues = append(issues, ConfigIssue{Severity: "error", Kind: "service-project", SubjectID: service.ID, Detail: "service references an unknown project"})
		}
		if service.EndpointKey == "" {
			issues = append(issues, ConfigIssue{Severity: "warn", Kind: "service-endpoint", SubjectID: service.ID, Detail: "service has no endpoint_key"})
			continue
		}
		serviceEndpointKeys[service.EndpointKey] = true
		if gatusErr == nil && !endpointKeys[service.EndpointKey] {
			issues = append(issues, ConfigIssue{Severity: "warn", Kind: "service-endpoint", SubjectID: service.ID, Detail: "endpoint_key was not found in Gatus"})
		}
	}
	for _, project := range a.config.Projects {
		for _, serviceID := range project.ServiceIDs {
			if projectServiceRefs[project.ID] == nil {
				projectServiceRefs[project.ID] = map[string]bool{}
			}
			projectServiceRefs[project.ID][serviceID] = true
			if !serviceIDs[serviceID] {
				issues = append(issues, ConfigIssue{Severity: "error", Kind: "project-service", SubjectID: project.ID, Detail: fmt.Sprintf("project references unknown service %s", serviceID)})
			}
		}
	}
	for _, service := range a.config.Services {
		if service.ProjectID != "" && projectIDs[service.ProjectID] && !projectServiceRefs[service.ProjectID][service.ID] {
			issues = append(issues, ConfigIssue{Severity: "warn", Kind: "service-project-list", SubjectID: service.ID, Detail: "service project_id is not listed in that project's service_ids"})
		}
	}
	if gatusErr == nil {
		for _, endpoint := range endpoints {
			if !serviceEndpointKeys[endpoint.Key] {
				issues = append(issues, ConfigIssue{Severity: "info", Kind: "gatus-orphan", SubjectID: endpoint.Key, Detail: "Gatus endpoint is not mapped to a service"})
			}
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Severity == issues[j].Severity {
			return issues[i].SubjectID < issues[j].SubjectID
		}
		return severityRank(issues[i].Severity) < severityRank(issues[j].Severity)
	})
	return issues
}

func (a *Aggregator) metricsDiagnostic(metrics []MetricsView) MetricsDiagnostic {
	expected := make([]string, 0, len(a.config.Nodes))
	for _, node := range a.config.Nodes {
		expected = append(expected, node.ID)
	}
	sort.Strings(expected)

	reportingMap := map[string]MetricsView{}
	for _, metric := range metrics {
		reportingMap[metric.NodeID] = metric
	}
	reporting := make([]string, 0, len(reportingMap))
	missing := []string{}
	stale := []string{}
	gpuNodes := []string{}
	collectorIssues := []MetricsCollectorIssue{}
	serviceResourceBudgets, serviceResourceIssues := a.serviceResourceBudgetStatuses(metrics)
	for _, nodeID := range expected {
		metric, ok := reportingMap[nodeID]
		if !ok {
			missing = append(missing, nodeID)
			continue
		}
		reporting = append(reporting, nodeID)
		if metric.Stale {
			stale = append(stale, nodeID)
		}
		if metric.GPU.Available {
			gpuNodes = append(gpuNodes, nodeID)
		}
		for _, collector := range metric.CollectorStatus {
			if collector.OK {
				continue
			}
			collectorIssues = append(collectorIssues, MetricsCollectorIssue{
				NodeID:    nodeID,
				Name:      collector.Name,
				Detail:    collector.Detail,
				ElapsedMS: collector.ElapsedMS,
			})
		}
	}
	sort.Strings(reporting)
	sort.Strings(stale)
	sort.Strings(gpuNodes)
	sort.Slice(collectorIssues, func(i, j int) bool {
		if collectorIssues[i].NodeID == collectorIssues[j].NodeID {
			return collectorIssues[i].Name < collectorIssues[j].Name
		}
		return collectorIssues[i].NodeID < collectorIssues[j].NodeID
	})
	collectorSummary := metricsCollectorSummary(metrics, reporting)
	return MetricsDiagnostic{
		ExpectedNodes:          expected,
		ReportingNodes:         reporting,
		MissingNodes:           missing,
		StaleNodes:             stale,
		GPUNodes:               gpuNodes,
		CollectorIssues:        collectorIssues,
		CollectorSummary:       collectorSummary,
		ServiceResourceBudgets: serviceResourceBudgets,
		ServiceResourceIssues:  serviceResourceIssues,
	}
}

func metricsCollectorSummary(metrics []MetricsView, reportingNodes []string) []MetricsCollectorSummary {
	collectorNames := map[string]bool{}
	statusesByNode := map[string]map[string]CollectorStatus{}
	for _, metric := range metrics {
		if statusesByNode[metric.NodeID] == nil {
			statusesByNode[metric.NodeID] = map[string]CollectorStatus{}
		}
		for _, collector := range metric.CollectorStatus {
			if collector.Name == "" {
				continue
			}
			collectorNames[collector.Name] = true
			statusesByNode[metric.NodeID][collector.Name] = collector
		}
	}
	names := sortedKeys(collectorNames)
	summaries := make([]MetricsCollectorSummary, 0, len(names))
	for _, name := range names {
		summary := MetricsCollectorSummary{
			Name:             name,
			Status:           StatusOK,
			ReportingNodes:   len(reportingNodes),
			CacheWarnSeconds: collectorCacheWarnSeconds(name),
			Detail:           "collector is healthy across reporting nodes",
		}
		var elapsedTotal int64
		var elapsedCount int
		for _, nodeID := range reportingNodes {
			collector, ok := statusesByNode[nodeID][name]
			if !ok {
				summary.MissingNodes = append(summary.MissingNodes, nodeID)
				continue
			}
			summary.ObservedNodes++
			elapsedTotal += collector.ElapsedMS
			elapsedCount++
			if collector.ElapsedMS > summary.MaxElapsedMS {
				summary.MaxElapsedMS = collector.ElapsedMS
			}
			if collector.OK {
				summary.OKNodes++
			} else {
				summary.FailedNodes++
				summary.FailedNodeIDs = append(summary.FailedNodeIDs, nodeID)
			}
			if collector.Cached {
				summary.CachedNodes++
				summary.CachedNodeIDs = append(summary.CachedNodeIDs, nodeID)
				if collector.CacheAgeSeconds > summary.MaxCacheAgeSeconds {
					summary.MaxCacheAgeSeconds = collector.CacheAgeSeconds
				}
				if summary.CacheWarnSeconds > 0 && collector.CacheAgeSeconds > summary.CacheWarnSeconds {
					summary.StaleCachedNodes++
					summary.StaleCachedNodeIDs = append(summary.StaleCachedNodeIDs, nodeID)
				}
			}
		}
		sort.Strings(summary.MissingNodes)
		sort.Strings(summary.FailedNodeIDs)
		sort.Strings(summary.CachedNodeIDs)
		sort.Strings(summary.StaleCachedNodeIDs)
		if elapsedCount > 0 {
			summary.AvgElapsedMS = float64(elapsedTotal) / float64(elapsedCount)
		}
		if summary.FailedNodes > 0 || len(summary.MissingNodes) > 0 || summary.StaleCachedNodes > 0 {
			summary.Status = StatusDegraded
			parts := []string{}
			if summary.FailedNodes > 0 {
				parts = append(parts, fmt.Sprintf("%d failed", summary.FailedNodes))
			}
			if len(summary.MissingNodes) > 0 {
				parts = append(parts, fmt.Sprintf("%d missing", len(summary.MissingNodes)))
			}
			if summary.StaleCachedNodes > 0 {
				parts = append(parts, fmt.Sprintf("%d stale cached", summary.StaleCachedNodes))
			}
			if summary.CachedNodes > 0 {
				parts = append(parts, fmt.Sprintf("%d cached", summary.CachedNodes))
			}
			summary.Detail = strings.Join(parts, "; ")
		} else if summary.CachedNodes > 0 {
			summary.Detail = fmt.Sprintf("collector is healthy; %d nodes used cached data", summary.CachedNodes)
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func collectorCacheWarnSeconds(name string) float64 {
	switch name {
	case "gpu":
		return 240
	case "containers", "processes":
		return 600
	default:
		return 0
	}
}

func (a *Aggregator) serviceResourceBudgetStatuses(metrics []MetricsView) ([]ServiceResourceBudgetStatus, []ServiceResourceIssue) {
	metricsByNode := map[string]MetricsView{}
	for _, metric := range metrics {
		metricsByNode[metric.NodeID] = metric
	}

	statuses := []ServiceResourceBudgetStatus{}
	issues := []ServiceResourceIssue{}
	for _, service := range a.config.Services {
		budget := service.ResourceBudget
		if budget == nil {
			continue
		}
		status := ServiceResourceBudgetStatus{
			ServiceID:      service.ID,
			ServiceName:    service.Name,
			NodeID:         service.NodeID,
			Status:         StatusOK,
			ContainerNames: append([]string{}, budget.ContainerNames...),
			MaxMemoryBytes: mibToBytes(budget.MaxMemoryMiB),
			MaxCPUPercent:  budget.MaxCPUPercent,
			Detail:         "within budget",
		}
		metric, ok := metricsByNode[service.NodeID]
		if !ok {
			status.Status = StatusUnknown
			status.Detail = "node metrics are missing"
			issues = append(issues, serviceResourceIssue(service, "warn", "metrics", 0, 0, "", "", status.Detail))
			statuses = append(statuses, status)
			continue
		}
		if !metric.Containers.Available {
			status.Status = StatusUnknown
			status.Detail = "container metrics are unavailable"
			issues = append(issues, serviceResourceIssue(service, "warn", "containers", 0, 0, "", "", status.Detail))
			statuses = append(statuses, status)
			continue
		}

		desired := map[string]bool{}
		for _, name := range budget.ContainerNames {
			desired[name] = true
		}
		found := map[string]bool{}
		for _, container := range metric.Containers.Containers {
			if budget.ComposeProject != "" && container.ComposeProject != budget.ComposeProject {
				continue
			}
			if len(desired) > 0 && !desired[container.Name] {
				continue
			}
			status.MatchedContainers = append(status.MatchedContainers, container.Name)
			found[container.Name] = true
			status.MemoryUsageBytes += container.MemoryUsageBytes
			status.CPUPercent += container.CPUPercent
		}

		for _, name := range budget.ContainerNames {
			if !found[name] {
				status.MissingContainers = append(status.MissingContainers, name)
			}
		}
		if len(status.MatchedContainers) == 0 {
			status.Status = StatusUnknown
			status.Detail = "no matching containers found"
			issues = append(issues, serviceResourceIssue(service, "warn", "containers", 0, 0, "", "", status.Detail))
		}
		if len(status.MissingContainers) > 0 {
			status.Status = StatusDegraded
			status.Detail = fmt.Sprintf("missing containers: %s", strings.Join(status.MissingContainers, ", "))
			for _, name := range status.MissingContainers {
				issues = append(issues, serviceResourceIssue(service, "warn", "container", 0, 0, "", name, "configured container was not found in latest metrics"))
			}
		}
		if status.MaxMemoryBytes > 0 {
			status.MemoryUsagePercent = float64(status.MemoryUsageBytes) / float64(status.MaxMemoryBytes) * 100
			status.MemoryHeadroomBytes = status.MaxMemoryBytes - status.MemoryUsageBytes
		}
		if status.MaxCPUPercent > 0 {
			status.CPUHeadroomPercent = status.MaxCPUPercent - status.CPUPercent
		}
		if status.MaxMemoryBytes > 0 && status.MemoryUsageBytes > status.MaxMemoryBytes {
			status.Status = StatusDegraded
			status.Detail = fmt.Sprintf("memory %.1fMiB exceeds %.1fMiB", bytesToMiB(status.MemoryUsageBytes), bytesToMiB(status.MaxMemoryBytes))
			issues = append(issues, serviceResourceIssue(service, "warn", "memory", bytesToMiB(status.MemoryUsageBytes), bytesToMiB(status.MaxMemoryBytes), "MiB", "", status.Detail))
		}
		if status.MaxCPUPercent > 0 && status.CPUPercent > status.MaxCPUPercent {
			status.Status = StatusDegraded
			status.Detail = fmt.Sprintf("CPU %.1f%% exceeds %.1f%%", status.CPUPercent, status.MaxCPUPercent)
			issues = append(issues, serviceResourceIssue(service, "warn", "cpu", status.CPUPercent, status.MaxCPUPercent, "%", "", status.Detail))
		}
		sort.Strings(status.MatchedContainers)
		sort.Strings(status.MissingContainers)
		statuses = append(statuses, status)
	}

	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].NodeID == statuses[j].NodeID {
			return statuses[i].ServiceID < statuses[j].ServiceID
		}
		return statuses[i].NodeID < statuses[j].NodeID
	})
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].NodeID == issues[j].NodeID {
			return issues[i].ServiceID < issues[j].ServiceID
		}
		return issues[i].NodeID < issues[j].NodeID
	})
	return statuses, issues
}

func serviceResourceIssue(service ServiceConfig, severity, metric string, value, limit float64, unit, containerName, detail string) ServiceResourceIssue {
	return ServiceResourceIssue{
		ServiceID:     service.ID,
		ServiceName:   service.Name,
		NodeID:        service.NodeID,
		Severity:      severity,
		Metric:        metric,
		Value:         value,
		Limit:         limit,
		Unit:          unit,
		ContainerName: containerName,
		Detail:        detail,
	}
}

func mibToBytes(value float64) int64 {
	if value <= 0 {
		return 0
	}
	return int64(math.Round(value * 1024 * 1024))
}

func bytesToMiB(value int64) float64 {
	if value <= 0 {
		return 0
	}
	return float64(value) / 1024 / 1024
}

func (a *Aggregator) nodeViews(services []ServiceView) []NodeView {
	byNode := map[string][]ServiceView{}
	for _, service := range services {
		byNode[service.NodeID] = append(byNode[service.NodeID], service)
	}

	nodes := make([]NodeView, 0, len(a.config.Nodes))
	for _, node := range a.config.Nodes {
		nodeServices := byNode[node.ID]
		status, detail, downCount, lastCheckedAt := aggregateServiceViews(nodeServices)
		nodes = append(nodes, NodeView{
			NodeConfig:    node,
			Status:        status,
			Detail:        detail,
			ServiceCount:  len(nodeServices),
			DownCount:     downCount,
			LastCheckedAt: lastCheckedAt,
		})
	}
	return nodes
}

func (a *Aggregator) projectViews(services []ServiceView) []ProjectView {
	byID := map[string]ServiceView{}
	for _, service := range services {
		byID[service.ID] = service
	}

	projects := make([]ProjectView, 0, len(a.config.Projects))
	for _, project := range a.config.Projects {
		projectServices := []ServiceView{}
		for _, serviceID := range project.ServiceIDs {
			if service, ok := byID[serviceID]; ok {
				projectServices = append(projectServices, service)
			}
		}
		status, detail, downCount, lastCheckedAt := aggregateServiceViews(projectServices)
		projects = append(projects, ProjectView{
			ProjectConfig: project,
			Status:        status,
			Detail:        detail,
			ServiceCount:  len(projectServices),
			DownCount:     downCount,
			LastCheckedAt: lastCheckedAt,
		})
	}
	return projects
}

func aggregateServiceViews(services []ServiceView) (Status, string, int, time.Time) {
	if len(services) == 0 {
		return StatusUnknown, "No services configured", 0, time.Time{}
	}
	statuses := []Status{}
	downCount := 0
	criticalDownCount := 0
	unknownCount := 0
	degradedCount := 0
	lastCheckedAt := time.Time{}
	for _, service := range services {
		statuses = append(statuses, service.Status)
		if service.LastCheckedAt.After(lastCheckedAt) {
			lastCheckedAt = service.LastCheckedAt
		}
		if service.Status == StatusDown {
			downCount++
			if service.Critical {
				criticalDownCount++
			}
		}
		if service.Status == StatusUnknown {
			unknownCount++
		}
		if service.Status == StatusDegraded {
			degradedCount++
		}
	}
	status := StatusOK
	if criticalDownCount > 0 {
		status = StatusDown
	} else if downCount > 0 || unknownCount > 0 || degradedCount > 0 {
		status = StatusDegraded
	}
	parts := []string{}
	if downCount > 0 {
		parts = append(parts, fmt.Sprintf("%d down", downCount))
	}
	if unknownCount > 0 {
		parts = append(parts, fmt.Sprintf("%d unknown", unknownCount))
	}
	if len(parts) == 0 {
		parts = append(parts, "All checks healthy")
	}
	return status, strings.Join(parts, ", "), downCount, lastCheckedAt
}

func aggregateOverall(statuses []Status) Status {
	if len(statuses) == 0 {
		return StatusUnknown
	}
	hasDown := false
	hasDegraded := false
	hasUnknown := false
	for _, status := range statuses {
		switch status {
		case StatusDown:
			hasDown = true
		case StatusDegraded:
			hasDegraded = true
		case StatusUnknown:
			hasUnknown = true
		}
	}
	if hasDown {
		return StatusDown
	}
	if hasDegraded || hasUnknown {
		return StatusDegraded
	}
	return StatusOK
}

func countStatuses(services []ServiceView) StatusCounts {
	var counts StatusCounts
	for _, service := range services {
		switch service.Status {
		case StatusOK:
			counts.OK++
		case StatusDegraded:
			counts.Degraded++
		case StatusDown:
			counts.Down++
		case StatusMaintenance:
			counts.Maintenance++
		default:
			counts.Unknown++
		}
	}
	return counts
}

func projectStatuses(projects []ProjectView) []Status {
	statuses := make([]Status, 0, len(projects))
	for _, project := range projects {
		statuses = append(statuses, project.Status)
	}
	return statuses
}

func failingServices(services []ServiceView) []ServiceView {
	failures := []ServiceView{}
	for _, service := range services {
		if service.Status != StatusOK && service.Status != StatusMaintenance {
			failures = append(failures, service)
		}
	}
	sort.Slice(failures, func(i, j int) bool {
		if failures[i].Status == failures[j].Status {
			return failures[i].ID < failures[j].ID
		}
		return string(failures[i].Status) < string(failures[j].Status)
	})
	return failures
}

func providerStatus(ok bool, hasData bool) Status {
	if !ok {
		return StatusDown
	}
	if !hasData {
		return StatusDegraded
	}
	return StatusOK
}

func providerDetail(err error, okDetail string) string {
	if err != nil {
		return err.Error()
	}
	return okDetail
}

func configStatus(issues []ConfigIssue) Status {
	status := StatusOK
	for _, issue := range issues {
		switch issue.Severity {
		case "error":
			return StatusDown
		case "warn":
			status = StatusDegraded
		}
	}
	return status
}

func metricsProviderStatus(err error, metrics MetricsDiagnostic) Status {
	if err != nil {
		return StatusDown
	}
	if len(metrics.ReportingNodes) == 0 {
		return StatusDown
	}
	if len(metrics.MissingNodes) > 0 || len(metrics.StaleNodes) > 0 || len(metrics.CollectorIssues) > 0 {
		return StatusDegraded
	}
	for _, collector := range metrics.CollectorSummary {
		if collector.Status != StatusOK {
			return StatusDegraded
		}
	}
	return StatusOK
}

func metricsProviderDetail(err error, metrics MetricsDiagnostic) string {
	if err != nil {
		return err.Error()
	}
	parts := []string{fmt.Sprintf("%d/%d nodes reporting", len(metrics.ReportingNodes), len(metrics.ExpectedNodes))}
	if len(metrics.MissingNodes) > 0 {
		parts = append(parts, "missing: "+strings.Join(metrics.MissingNodes, ", "))
	}
	if len(metrics.StaleNodes) > 0 {
		parts = append(parts, "stale: "+strings.Join(metrics.StaleNodes, ", "))
	}
	degradedCollectors := []string{}
	for _, collector := range metrics.CollectorSummary {
		if collector.Status != StatusOK {
			degradedCollectors = append(degradedCollectors, collector.Name)
		}
	}
	if len(degradedCollectors) > 0 {
		sort.Strings(degradedCollectors)
		parts = append(parts, "collector degraded: "+strings.Join(degradedCollectors, ", "))
	}
	return strings.Join(parts, "; ")
}

func severityRank(severity string) int {
	switch severity {
	case "error":
		return 0
	case "warn":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}
