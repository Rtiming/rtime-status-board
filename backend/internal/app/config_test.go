package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	cfg, err := LoadConfig("../../../config/status-board.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if got := len(cfg.Nodes); got != 5 {
		t.Fatalf("nodes = %d, want 5", got)
	}
	if got := len(cfg.Projects); got == 0 {
		t.Fatalf("expected projects")
	}
	khoj := ServiceConfig{}
	for _, service := range cfg.Services {
		if service.ID == "orangepi-khoj" {
			khoj = service
			break
		}
	}
	if khoj.ResourceBudget == nil || len(khoj.ResourceBudget.ContainerNames) != 2 || khoj.ResourceBudget.MaxMemoryMiB != 1536 {
		t.Fatalf("orangepi-khoj resource budget = %#v, want two containers and 1536MiB", khoj.ResourceBudget)
	}
	thresholds := cfg.Diagnostics.ResourceThresholds.EffectiveForNode("srv03")
	if thresholds.CPUPercent != 90 || thresholds.MemoryPercent != 90 || thresholds.RootDiskPercent != 85 || thresholds.GPUUtilPercent != 90 ||
		thresholds.NetworkRXBps != 52428800 || thresholds.NetworkTXBps != 52428800 ||
		thresholds.StorageReadBps != 104857600 || thresholds.StorageWriteBps != 104857600 {
		t.Fatalf("resource thresholds = %#v, want default YAML values", thresholds)
	}
}

func TestAggregateOverall(t *testing.T) {
	cases := []struct {
		name string
		in   []Status
		want Status
	}{
		{name: "all ok", in: []Status{StatusOK, StatusOK}, want: StatusOK},
		{name: "unknown degrades", in: []Status{StatusOK, StatusUnknown}, want: StatusDegraded},
		{name: "down wins", in: []Status{StatusOK, StatusDegraded, StatusDown}, want: StatusDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateOverall(tc.in); got != tc.want {
				t.Fatalf("aggregateOverall() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestValidateRejectsInvalidServiceResourceBudget(t *testing.T) {
	cfg := AppConfig{
		App:   AppMeta{Name: "test"},
		Nodes: []NodeConfig{{ID: "node-a", Name: "node-a"}},
		Projects: []ProjectConfig{
			{ID: "project-a", Name: "Project A", ServiceIDs: []string{"svc-a"}},
		},
		Services: []ServiceConfig{
			{
				ID:        "svc-a",
				Name:      "svc-a",
				NodeID:    "node-a",
				ProjectID: "project-a",
				ResourceBudget: &ServiceResourceBudget{
					MaxMemoryMiB: 100,
				},
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("Validate succeeded, want resource_budget container mapping error")
	}
}

func TestValidateRejectsInvalidResourceThresholds(t *testing.T) {
	cases := []struct {
		name string
		cfg  AppConfig
	}{
		{
			name: "global threshold too high",
			cfg: AppConfig{
				App:   AppMeta{Name: "test"},
				Nodes: []NodeConfig{{ID: "node-a", Name: "node-a"}},
				Diagnostics: DiagnosticsConfig{ResourceThresholds: ResourceThresholdConfig{
					CPUPercent: floatPtr(101),
				}},
			},
		},
		{
			name: "node override references missing node",
			cfg: AppConfig{
				App:   AppMeta{Name: "test"},
				Nodes: []NodeConfig{{ID: "node-a", Name: "node-a"}},
				Diagnostics: DiagnosticsConfig{ResourceThresholds: ResourceThresholdConfig{
					Nodes: map[string]ResourceThresholdOverride{
						"node-b": {CPUPercent: floatPtr(80)},
					},
				}},
			},
		},
		{
			name: "network threshold is zero",
			cfg: AppConfig{
				App:   AppMeta{Name: "test"},
				Nodes: []NodeConfig{{ID: "node-a", Name: "node-a"}},
				Diagnostics: DiagnosticsConfig{ResourceThresholds: ResourceThresholdConfig{
					NetworkRXBps: floatPtr(0),
				}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatalf("Validate succeeded, want threshold error")
			}
		})
	}
}

func TestDeploymentDiagnosticDevelopmentChecks(t *testing.T) {
	frontendDir := t.TempDir()
	writeFile(t, filepath.Join(frontendDir, "index.html"), "<!doctype html>")
	aggregator := NewAggregatorWithRuntime(&AppConfig{App: AppMeta{Name: "test"}}, nil, nil, 10*time.Second, RuntimeSettings{
		DeploymentMode:   "development",
		ConfigPath:       "../../../config/status-board.yaml",
		DBPath:           "./data/status-board.dev.db",
		GatusURL:         "http://gatus:8080/",
		ListenAddr:       ":8080",
		FrontendDir:      frontendDir,
		CacheTTL:         10 * time.Second,
		MetricsRetention: 30 * 24 * time.Hour,
	})
	diag := aggregator.deploymentDiagnostic(StoreDiagnostic{TotalSizeBytes: 2 << 20})
	if diag.Status != StatusOK {
		t.Fatalf("deployment status = %s (%#v), want ok", diag.Status, diag.Checks)
	}
	if diag.Mode != "development" {
		t.Fatalf("deployment mode = %q, want development", diag.Mode)
	}
	if len(diag.Checks) == 0 {
		t.Fatalf("deployment checks empty")
	}
}

func TestDeploymentDiagnosticProductionBoundaryIssue(t *testing.T) {
	frontendDir := t.TempDir()
	writeFile(t, filepath.Join(frontendDir, "index.html"), "<!doctype html>")
	aggregator := NewAggregatorWithRuntime(&AppConfig{App: AppMeta{Name: "test"}}, nil, nil, 10*time.Second, RuntimeSettings{
		DeploymentMode:   "production",
		ConfigPath:       "/app/config/status-board.yaml",
		DBPath:           "/data/status-board.db",
		GatusURL:         "http://127.0.0.1:23181",
		ListenAddr:       ":8080",
		FrontendDir:      frontendDir,
		CacheTTL:         10 * time.Second,
		MetricsRetention: 30 * 24 * time.Hour,
	})
	diag := aggregator.deploymentDiagnostic(StoreDiagnostic{})
	if diag.Status != StatusDegraded {
		t.Fatalf("deployment status = %s, want degraded", diag.Status)
	}
	var listen DeploymentCheck
	for _, check := range diag.Checks {
		if check.Key == "listen-addr" {
			listen = check
			break
		}
	}
	if listen.Status != StatusDegraded || listen.Expected != "127.0.0.1:23180" || listen.Actual != ":8080" {
		t.Fatalf("listen check = %#v, want degraded production bind mismatch", listen)
	}
}

func TestPublicDomainDNSCheck(t *testing.T) {
	cases := []struct {
		name       string
		ips        []string
		err        error
		wantStatus Status
		wantDetail string
	}{
		{
			name:       "expected public ip",
			ips:        []string{"203.0.113.10"},
			wantStatus: StatusOK,
			wantDetail: "public domain resolves to the expected public IP",
		},
		{
			name:       "fake ip warning",
			ips:        []string{"198.18.0.136"},
			wantStatus: StatusDegraded,
			wantDetail: "public domain resolved to a local proxy fake-IP range instead of the expected public IP",
		},
		{
			name:       "lookup error",
			err:        context.DeadlineExceeded,
			wantStatus: StatusDegraded,
			wantDetail: "public domain DNS lookup failed from the statusd runtime",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := publicDomainDNSCheck("status.example.com", "203.0.113.10", func(context.Context, string) ([]string, error) {
				return tc.ips, tc.err
			})
			if check.Status != tc.wantStatus || check.Detail != tc.wantDetail {
				t.Fatalf("check = %#v, want status %s detail %q", check, tc.wantStatus, tc.wantDetail)
			}
		})
	}
}

func TestPublicIPEntryCheck(t *testing.T) {
	cases := []struct {
		name       string
		publicIP   string
		wantStatus Status
		wantDetail string
	}{
		{
			name:       "valid public ip",
			publicIP:   "203.0.113.10",
			wantStatus: StatusOK,
			wantDetail: "public IP entry is configured; verify-sh-core checks that unauthenticated access returns 401",
		},
		{
			name:       "missing public ip",
			publicIP:   "",
			wantStatus: StatusDegraded,
			wantDetail: "expected public IP is not configured",
		},
		{
			name:       "invalid public ip",
			publicIP:   "status.example.com",
			wantStatus: StatusDegraded,
			wantDetail: "expected public IP is not a valid IP address",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := publicIPEntryCheck(tc.publicIP)
			if check.Status != tc.wantStatus || check.Detail != tc.wantDetail {
				t.Fatalf("check = %#v, want status %s detail %q", check, tc.wantStatus, tc.wantDetail)
			}
		})
	}
}

func TestProjectDiagnosticsCoverageIssues(t *testing.T) {
	cfg := AppConfig{App: AppMeta{Name: "test"}}
	aggregator := NewAggregator(&cfg, nil, nil, 0)
	base := time.Date(2026, 6, 10, 7, 45, 0, 0, time.UTC)
	projects := []ProjectView{
		{ProjectConfig: ProjectConfig{ID: "project-a", Name: "Project A"}},
	}
	services := []ServiceView{
		{ServiceConfig: ServiceConfig{ID: "svc-ok", Name: "OK", ProjectID: "project-a", NodeID: "node-a", EndpointKey: "endpoint-ok", Critical: true}, Status: StatusOK},
		{ServiceConfig: ServiceConfig{ID: "svc-missing", Name: "Missing", ProjectID: "project-a", NodeID: "node-b", EndpointKey: "endpoint-missing"}, Status: StatusOK},
		{ServiceConfig: ServiceConfig{ID: "svc-unmapped", Name: "Unmapped", ProjectID: "project-a", NodeID: "node-c"}, Status: StatusOK},
		{ServiceConfig: ServiceConfig{ID: "svc-down", Name: "Down", ProjectID: "project-a", NodeID: "node-d", EndpointKey: "endpoint-down", Critical: true}, Status: StatusDown},
	}
	diag := aggregator.projectDiagnostics(projects, services, MetricsDiagnostic{
		ReportingNodes: []string{"node-a"},
		MissingNodes:   []string{"node-b"},
		StaleNodes:     []string{"node-d"},
	}, []RuntimeEndpointStatus{
		{Key: "endpoint-ok", RecentResults: 3, RecentFailures: 0, LastCheckedAt: base.Add(-2 * time.Minute)},
		{Key: "endpoint-down", RecentResults: 4, RecentFailures: 2, LastCheckedAt: base},
	}, []Event{
		{Kind: "project", SubjectID: "project-a", CreatedAt: base.Add(-5 * time.Minute)},
		{Kind: "service", SubjectID: "svc-down", CreatedAt: base.Add(-3 * time.Minute)},
		{Kind: "node", SubjectID: "node-a", CreatedAt: base.Add(-time.Minute)},
		{Kind: "project", SubjectID: "other-project", CreatedAt: base},
	})
	if len(diag) != 1 {
		t.Fatalf("project diagnostics = %#v, want one row", diag)
	}
	row := diag[0]
	if row.Status != StatusDown {
		t.Fatalf("project diagnostic status = %s, want down", row.Status)
	}
	if row.ServiceCount != 4 || row.CriticalServiceCount != 2 || row.EndpointCount != 2 || row.MissingEndpointCount != 1 || row.UnmappedServiceCount != 1 || row.DownServiceCount != 1 {
		t.Fatalf("project diagnostic counts = %#v, want coverage issue counts", row)
	}
	if got := strings.Join(row.MetricsReportingNodes, ","); got != "node-a" {
		t.Fatalf("reporting nodes = %q, want node-a", got)
	}
	if got := strings.Join(row.MetricsMissingNodes, ","); got != "node-b" {
		t.Fatalf("missing metric nodes = %q, want node-b", got)
	}
	if got := strings.Join(row.MetricsStaleNodes, ","); got != "node-d" {
		t.Fatalf("stale metric nodes = %q, want node-d", got)
	}
	if !strings.Contains(row.Detail, "endpoint mappings are missing") || !strings.Contains(row.Detail, "services have no endpoint key") {
		t.Fatalf("detail = %q, want endpoint coverage detail", row.Detail)
	}
	if row.RecentCheckCount != 7 || row.RecentFailureCount != 2 || row.LastCheckAt == nil || !row.LastCheckAt.Equal(base) {
		t.Fatalf("recent check summary = %#v, want 7 checks, 2 failures, latest %s", row, base)
	}
	if row.RecentEventCount != 3 || row.LastEventAt == nil || !row.LastEventAt.Equal(base.Add(-time.Minute)) {
		t.Fatalf("recent event summary = %#v, want 3 related events and latest node event", row)
	}
}

func TestMetricsDiagnosticServiceResourceBudgets(t *testing.T) {
	cfg := AppConfig{
		App:   AppMeta{Name: "test"},
		Nodes: []NodeConfig{{ID: "orangepi", Name: "orangepi"}},
		Projects: []ProjectConfig{
			{ID: "knowledge-base", Name: "Knowledge Base", ServiceIDs: []string{"orangepi-khoj"}},
		},
		Services: []ServiceConfig{
			{
				ID:        "orangepi-khoj",
				Name:      "Khoj web",
				NodeID:    "orangepi",
				ProjectID: "knowledge-base",
				ResourceBudget: &ServiceResourceBudget{
					ContainerNames: []string{"khoj-server-1", "khoj-database-1"},
					ComposeProject: "khoj",
					MaxMemoryMiB:   1536,
					MaxCPUPercent:  50,
				},
			},
		},
	}
	aggregator := NewAggregator(&cfg, nil, nil, 0)
	diag := aggregator.metricsDiagnostic([]MetricsView{
		{
			MetricsReport: MetricsReport{NodeID: "orangepi"},
			Containers: ContainerMetrics{
				Available: true,
				Provider:  "docker",
				Containers: []ContainerMetric{
					{Name: "khoj-server-1", ComposeProject: "khoj", CPUPercent: 0.5, MemoryUsageBytes: 944 << 20},
					{Name: "khoj-database-1", ComposeProject: "khoj", CPUPercent: 0.1, MemoryUsageBytes: 34 << 20},
					{Name: "other", ComposeProject: "other", CPUPercent: 99, MemoryUsageBytes: 999 << 20},
				},
			},
		},
	})
	if len(diag.ServiceResourceBudgets) != 1 {
		t.Fatalf("resource budgets = %#v, want one budget", diag.ServiceResourceBudgets)
	}
	budget := diag.ServiceResourceBudgets[0]
	if budget.Status != StatusOK || budget.MemoryUsageBytes != 978<<20 || len(budget.MatchedContainers) != 2 {
		t.Fatalf("budget = %#v, want ok 978MiB from two Khoj containers", budget)
	}
	if len(diag.ServiceResourceIssues) != 0 {
		t.Fatalf("resource issues = %#v, want none", diag.ServiceResourceIssues)
	}
}

func TestMetricsDiagnosticCollectorSummary(t *testing.T) {
	cfg := AppConfig{
		App: AppMeta{Name: "test"},
		Nodes: []NodeConfig{
			{ID: "node-a", Name: "node-a"},
			{ID: "node-b", Name: "node-b"},
			{ID: "node-c", Name: "node-c"},
		},
	}
	aggregator := NewAggregator(&cfg, nil, nil, 0)
	diag := aggregator.metricsDiagnostic([]MetricsView{
		{
			MetricsReport: MetricsReport{NodeID: "node-a"},
			CollectorStatus: []CollectorStatus{
				{Name: "cpu", OK: true, ElapsedMS: 10},
				{Name: "gpu", OK: false, Detail: "nvidia-smi timeout", ElapsedMS: 20, Cached: true, CacheAgeSeconds: 61},
			},
		},
		{
			MetricsReport: MetricsReport{NodeID: "node-b"},
			CollectorStatus: []CollectorStatus{
				{Name: "cpu", OK: true, ElapsedMS: 30},
				{Name: "containers", OK: true, ElapsedMS: 5, Cached: true, CacheAgeSeconds: 120},
			},
		},
	})

	byName := map[string]MetricsCollectorSummary{}
	for _, row := range diag.CollectorSummary {
		byName[row.Name] = row
	}
	cpu := byName["cpu"]
	if cpu.Status != StatusOK || cpu.ReportingNodes != 2 || cpu.ObservedNodes != 2 || cpu.OKNodes != 2 || cpu.AvgElapsedMS != 20 || cpu.MaxElapsedMS != 30 {
		t.Fatalf("cpu summary = %#v, want healthy coverage across two reporting nodes", cpu)
	}
	gpu := byName["gpu"]
	if gpu.Status != StatusDegraded || gpu.ObservedNodes != 1 || gpu.FailedNodes != 1 || len(gpu.MissingNodes) != 1 || gpu.MissingNodes[0] != "node-b" ||
		len(gpu.FailedNodeIDs) != 1 || gpu.FailedNodeIDs[0] != "node-a" || gpu.CachedNodes != 1 || gpu.MaxCacheAgeSeconds != 61 {
		t.Fatalf("gpu summary = %#v, want failure, missing node-b, and cached node-a", gpu)
	}
	containers := byName["containers"]
	if containers.Status != StatusDegraded || containers.ObservedNodes != 1 || len(containers.MissingNodes) != 1 || containers.MissingNodes[0] != "node-a" ||
		containers.CachedNodes != 1 || containers.MaxCacheAgeSeconds != 120 {
		t.Fatalf("containers summary = %#v, want missing node-a and cached node-b", containers)
	}
}

func TestMetricsDiagnosticServiceResourceBudgetIssues(t *testing.T) {
	cfg := AppConfig{
		App:   AppMeta{Name: "test"},
		Nodes: []NodeConfig{{ID: "orangepi", Name: "orangepi"}},
		Services: []ServiceConfig{
			{
				ID:     "heavy",
				Name:   "Heavy Service",
				NodeID: "orangepi",
				ResourceBudget: &ServiceResourceBudget{
					ContainerNames: []string{"heavy-a", "heavy-b"},
					MaxMemoryMiB:   100,
					MaxCPUPercent:  10,
				},
			},
		},
	}
	aggregator := NewAggregator(&cfg, nil, nil, 0)
	diag := aggregator.metricsDiagnostic([]MetricsView{
		{
			MetricsReport: MetricsReport{NodeID: "orangepi"},
			Containers: ContainerMetrics{
				Available: true,
				Provider:  "docker",
				Containers: []ContainerMetric{
					{Name: "heavy-a", CPUPercent: 15, MemoryUsageBytes: 200 << 20},
				},
			},
		},
	})
	if len(diag.ServiceResourceBudgets) != 1 || diag.ServiceResourceBudgets[0].Status != StatusDegraded {
		t.Fatalf("budget status = %#v, want degraded", diag.ServiceResourceBudgets)
	}
	if len(diag.ServiceResourceIssues) != 3 {
		t.Fatalf("resource issues = %#v, want missing container, memory, and cpu issues", diag.ServiceResourceIssues)
	}
}

func TestOpsDiagnosticAggregatesActionableIssues(t *testing.T) {
	cfg := AppConfig{App: AppMeta{Name: "test"}}
	aggregator := NewAggregator(&cfg, nil, nil, 0)
	now := time.Date(2026, 6, 10, 6, 0, 0, 0, time.UTC)
	services := []ServiceView{
		{
			ServiceConfig: ServiceConfig{ID: "svc-critical", Name: "Critical Service", NodeID: "node-a", ProjectID: "project-a", Critical: true},
			Status:        StatusDown,
			Detail:        "connection refused",
			LastCheckedAt: now.Add(-time.Minute),
		},
		{
			ServiceConfig: ServiceConfig{ID: "svc-ok", Name: "OK Service", NodeID: "node-a"},
			Status:        StatusOK,
			Detail:        "Healthy",
			LastCheckedAt: now.Add(-time.Minute),
		},
	}
	metrics := []MetricsView{
		{
			MetricsReport: MetricsReport{
				NodeID: "node-a",
				CPU:    CPUMetrics{Percent: 91},
				Memory: MemoryMetrics{Percent: 92},
				Disk:   DiskMetrics{Percent: 86},
			},
			GPU: GPUMetrics{
				Available: true,
				Devices: []GPUDeviceMetric{
					{Name: "gpu0", UtilPercent: 95},
				},
			},
			UpdatedAt: now.Add(-2 * time.Minute),
		},
	}
	metricDiag := MetricsDiagnostic{
		ExpectedNodes:  []string{"node-a", "node-b"},
		ReportingNodes: []string{"node-a"},
		MissingNodes:   []string{"node-b"},
		CollectorIssues: []MetricsCollectorIssue{
			{NodeID: "node-a", Name: "containers", Detail: "permission denied", ElapsedMS: 12},
		},
		ServiceResourceIssues: []ServiceResourceIssue{
			{ServiceID: "svc-heavy", ServiceName: "Heavy Service", NodeID: "node-a", Severity: "warn", Metric: "memory", Value: 200, Limit: 100, Unit: "MiB", Detail: "memory exceeds budget"},
		},
	}
	ops := aggregator.opsDiagnostic(now, services, metrics, metricDiag, []ConfigIssue{{Severity: "warn", Kind: "service-endpoint", SubjectID: "svc-missing", Detail: "endpoint missing"}})

	kinds := map[string]bool{}
	for _, issue := range ops.Issues {
		kinds[issue.Kind] = true
	}
	for _, want := range []string{"service-check", "metrics-missing", "collector", "service-resource", "config-service-endpoint", "resource-cpu", "resource-memory", "resource-disk", "resource-gpu"} {
		if !kinds[want] {
			t.Fatalf("ops issue kinds = %#v, missing %s", kinds, want)
		}
	}
	if ops.Counts.Error != 1 {
		t.Fatalf("error count = %d, want critical service failure only", ops.Counts.Error)
	}
	if ops.Counts.Warn < 8 {
		t.Fatalf("warn count = %d, want threshold and diagnostic warnings", ops.Counts.Warn)
	}
}

func TestOpsDiagnosticUsesConfiguredResourceThresholds(t *testing.T) {
	cfg := AppConfig{
		App:   AppMeta{Name: "test"},
		Nodes: []NodeConfig{{ID: "node-a", Name: "node-a"}},
		Diagnostics: DiagnosticsConfig{ResourceThresholds: ResourceThresholdConfig{
			CPUPercent:      floatPtr(95),
			MemoryPercent:   floatPtr(93),
			RootDiskPercent: floatPtr(88),
			GPUUtilPercent:  floatPtr(96),
			NetworkRXBps:    floatPtr(1000),
			NetworkTXBps:    floatPtr(2000),
			StorageReadBps:  floatPtr(3000),
			StorageWriteBps: floatPtr(4000),
			Nodes: map[string]ResourceThresholdOverride{
				"node-a": {
					CPUPercent:      floatPtr(80),
					RootDiskPercent: floatPtr(70),
					NetworkTXBps:    floatPtr(500),
					StorageWriteBps: floatPtr(900),
				},
			},
		}},
	}
	aggregator := NewAggregator(&cfg, nil, nil, 0)
	now := time.Date(2026, 6, 10, 6, 30, 0, 0, time.UTC)
	metrics := []MetricsView{
		{
			MetricsReport: MetricsReport{
				NodeID: "node-a",
				CPU:    CPUMetrics{Percent: 85},
				Memory: MemoryMetrics{Percent: 94},
				Disk:   DiskMetrics{Percent: 75},
				Network: NetworkMetrics{
					RXBps: 1500,
					TXBps: 700,
				},
			},
			Storage: StorageMetrics{
				ReadBps:  2500,
				WriteBps: 1000,
			},
			GPU: GPUMetrics{
				Available: true,
				Devices: []GPUDeviceMetric{
					{Name: "gpu0", UtilPercent: 95},
				},
			},
			UpdatedAt: now,
		},
	}
	ops := aggregator.opsDiagnostic(
		now,
		[]ServiceView{{ServiceConfig: ServiceConfig{ID: "svc-ok", Name: "OK Service", NodeID: "node-a"}, Status: StatusOK}},
		metrics,
		MetricsDiagnostic{ExpectedNodes: []string{"node-a"}, ReportingNodes: []string{"node-a"}},
		nil,
	)

	limitsByKind := map[string]float64{}
	for _, issue := range ops.Issues {
		limitsByKind[issue.Kind] = issue.Limit
	}
	if limitsByKind["resource-cpu"] != 80 {
		t.Fatalf("resource-cpu limit = %.1f, want node override 80", limitsByKind["resource-cpu"])
	}
	if limitsByKind["resource-memory"] != 93 {
		t.Fatalf("resource-memory limit = %.1f, want global override 93", limitsByKind["resource-memory"])
	}
	if limitsByKind["resource-disk"] != 70 {
		t.Fatalf("resource-disk limit = %.1f, want node override 70", limitsByKind["resource-disk"])
	}
	if _, ok := limitsByKind["resource-gpu"]; ok {
		t.Fatalf("resource-gpu issue present, want no GPU alert under configured 96%% threshold")
	}
	if limitsByKind["resource-network-rx"] != 1000 {
		t.Fatalf("resource-network-rx limit = %.1f, want global threshold 1000", limitsByKind["resource-network-rx"])
	}
	if limitsByKind["resource-network-tx"] != 500 {
		t.Fatalf("resource-network-tx limit = %.1f, want node override 500", limitsByKind["resource-network-tx"])
	}
	if _, ok := limitsByKind["resource-storage-read"]; ok {
		t.Fatalf("resource-storage-read issue present, want no storage read alert below configured 3000 B/s threshold")
	}
	if limitsByKind["resource-storage-write"] != 900 {
		t.Fatalf("resource-storage-write limit = %.1f, want node override 900", limitsByKind["resource-storage-write"])
	}
	if len(ops.ResourceThresholds) != 1 || ops.ResourceThresholds[0].CPUPercent != 80 || ops.ResourceThresholds[0].GPUUtilPercent != 96 ||
		ops.ResourceThresholds[0].NetworkTXBps != 500 || ops.ResourceThresholds[0].StorageWriteBps != 900 {
		t.Fatalf("effective thresholds = %#v, want node and global overrides exposed", ops.ResourceThresholds)
	}
	if len(ops.ResourceStates) != 1 {
		t.Fatalf("resource states = %#v, want one node state", ops.ResourceStates)
	}
	state := ops.ResourceStates[0]
	if state.NodeID != "node-a" || state.Status != StatusDegraded {
		t.Fatalf("resource state = %#v, want node-a degraded", state)
	}
	if state.CPU.Value != 85 || state.CPU.Limit != 80 || state.CPU.Headroom != -5 {
		t.Fatalf("cpu headroom = %#v, want 85/80/-5", state.CPU)
	}
	if state.NetworkTX.Value != 700 || state.NetworkTX.Limit != 500 || state.NetworkTX.Headroom != -200 {
		t.Fatalf("network tx headroom = %#v, want 700/500/-200", state.NetworkTX)
	}
	if state.StorageRead.Value != 2500 || state.StorageRead.Limit != 3000 || state.StorageRead.Headroom != 500 {
		t.Fatalf("storage read headroom = %#v, want 2500/3000/500", state.StorageRead)
	}
	if !state.GPUAvailable || state.GPU.Value != 95 || state.GPU.Limit != 96 || state.GPU.Headroom != 1 {
		t.Fatalf("gpu headroom = available:%v %#v, want 95/96/1", state.GPUAvailable, state.GPU)
	}
}

func TestOpsDiagnosticIsEmptyWhenHealthy(t *testing.T) {
	cfg := AppConfig{App: AppMeta{Name: "test"}}
	aggregator := NewAggregator(&cfg, nil, nil, 0)
	now := time.Date(2026, 6, 10, 6, 0, 0, 0, time.UTC)
	services := []ServiceView{{ServiceConfig: ServiceConfig{ID: "svc-ok", Name: "OK Service", NodeID: "node-a"}, Status: StatusOK, Detail: "Healthy"}}
	metrics := []MetricsView{{MetricsReport: MetricsReport{NodeID: "node-a", CPU: CPUMetrics{Percent: 12}, Memory: MemoryMetrics{Percent: 34}, Disk: DiskMetrics{Percent: 45}}, UpdatedAt: now}}
	metricDiag := MetricsDiagnostic{ExpectedNodes: []string{"node-a"}, ReportingNodes: []string{"node-a"}}
	ops := aggregator.opsDiagnostic(now, services, metrics, metricDiag, nil)
	if len(ops.Issues) != 0 || ops.Counts.Error != 0 || ops.Counts.Warn != 0 || ops.Counts.Info != 0 {
		t.Fatalf("ops = %#v, want no issues", ops)
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func TestGatusResultDetailUsesErrorsArray(t *testing.T) {
	result := gatusResult{
		Success: false,
		Errors:  []string{"dial tcp 100.64.10.8:8080: connect: connection refused"},
		ConditionResults: []EndpointConditionResult{
			{Condition: "[STATUS] (0) == 200", Success: false},
		},
	}
	if got, want := result.detail(), "dial tcp 100.64.10.8:8080: connect: connection refused"; got != want {
		t.Fatalf("detail() = %q, want %q", got, want)
	}
}

func TestGatusResultDetailFallsBackToFailedConditions(t *testing.T) {
	result := gatusResult{
		Success: false,
		ConditionResults: []EndpointConditionResult{
			{Condition: "[CONNECTED] (false) == true", Success: false},
		},
	}
	if got, want := result.detail(), "Failed conditions: [CONNECTED] (false) == true"; got != want {
		t.Fatalf("detail() = %q, want %q", got, want)
	}
}

func TestGatusEndpointResultsMapsRecentResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/endpoints/statuses" {
			t.Fatalf("path = %s, want /api/v1/endpoints/statuses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"name": "khoj",
				"group": "orangepi",
				"key": "orangepi_khoj",
				"results": [
					{
						"duration": 16000000,
						"success": false,
							"errors": ["dial tcp 100.64.10.8:8080: connect: connection refused"],
						"conditionResults": [{"condition": "[STATUS] == 200", "success": false}],
						"timestamp": "2026-06-10T02:40:00Z"
					}
				]
			}
		]`))
	}))
	defer server.Close()

	results, err := NewGatusClient(server.URL).EndpointResults(context.Background(), "orangepi_khoj")
	if err != nil {
		t.Fatalf("endpoint results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Status != StatusDown || results[0].Success {
		t.Fatalf("result status/success = %s/%v, want down/false", results[0].Status, results[0].Success)
	}
	if results[0].ResponseTimeMS != 16 {
		t.Fatalf("response time = %d, want 16", results[0].ResponseTimeMS)
	}
	if got := results[0].Detail; got != "dial tcp 100.64.10.8:8080: connect: connection refused" {
		t.Fatalf("detail = %q", got)
	}
}

func TestParseHistoryWindowSupportsDaysAndClamps(t *testing.T) {
	cases := []struct {
		name   string
		window string
		want   time.Duration
	}{
		{name: "default", window: "", want: time.Hour},
		{name: "hours", window: "6h", want: 6 * time.Hour},
		{name: "days", window: "7d", want: 7 * 24 * time.Hour},
		{name: "uppercase days", window: " 2D ", want: 2 * 24 * time.Hour},
		{name: "non-positive defaults", window: "0s", want: time.Hour},
		{name: "clamps long windows", window: "45d", want: 30 * 24 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHistoryWindow(tc.window)
			if err != nil {
				t.Fatalf("parseHistoryWindow(%q): %v", tc.window, err)
			}
			if got != tc.want {
				t.Fatalf("parseHistoryWindow(%q) = %s, want %s", tc.window, got, tc.want)
			}
		})
	}
}

func TestParseLimitDefaultsAndClamps(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{raw: "", want: 30},
		{raw: "bad", want: 30},
		{raw: "0", want: 30},
		{raw: "7", want: 7},
		{raw: "500", want: 100},
	}
	for _, tc := range cases {
		if got := parseLimit(tc.raw, 30, 100); got != tc.want {
			t.Fatalf("parseLimit(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestProjectDetailFromSummaryFiltersRelatedData(t *testing.T) {
	now := time.Date(2026, 6, 10, 1, 0, 0, 0, time.UTC)
	summary := &SummaryResponse{
		GeneratedAt: now,
		Projects: []ProjectView{
			{ProjectConfig: ProjectConfig{ID: "knowledge-base", Name: "Knowledge Base"}, Status: StatusDegraded},
			{ProjectConfig: ProjectConfig{ID: "edge-dev", Name: "Edge Development"}, Status: StatusOK},
		},
		Nodes: []NodeView{
			{NodeConfig: NodeConfig{ID: "orangepi", Name: "orangepi"}, Status: StatusDegraded},
			{NodeConfig: NodeConfig{ID: "srv03", Name: "srv03"}, Status: StatusOK},
		},
		Services: []ServiceView{
			{ServiceConfig: ServiceConfig{ID: "orangepi-khoj", Name: "Khoj web", NodeID: "orangepi", ProjectID: "knowledge-base"}, Status: StatusDown},
			{ServiceConfig: ServiceConfig{ID: "srv03-ollama", Name: "Ollama", NodeID: "srv03", ProjectID: "edge-dev"}, Status: StatusOK},
		},
		Metrics: []MetricsView{
			{MetricsReport: MetricsReport{NodeID: "orangepi"}},
			{MetricsReport: MetricsReport{NodeID: "srv03"}},
		},
		Events: []Event{
			{Kind: "project", SubjectID: "knowledge-base", Label: "Knowledge Base", To: StatusDegraded, CreatedAt: now},
			{Kind: "service", SubjectID: "orangepi-khoj", Label: "Khoj web", To: StatusDown, CreatedAt: now},
			{Kind: "service", SubjectID: "srv03-ollama", Label: "Ollama", To: StatusOK, CreatedAt: now},
		},
	}

	detail, err := projectDetailFromSummary(summary, "knowledge-base")
	if err != nil {
		t.Fatalf("project detail: %v", err)
	}
	if detail.Project.ID != "knowledge-base" {
		t.Fatalf("project id = %s, want knowledge-base", detail.Project.ID)
	}
	if len(detail.Services) != 1 || detail.Services[0].ID != "orangepi-khoj" {
		t.Fatalf("services = %#v, want only orangepi-khoj", detail.Services)
	}
	if len(detail.Nodes) != 1 || detail.Nodes[0].ID != "orangepi" {
		t.Fatalf("nodes = %#v, want only orangepi", detail.Nodes)
	}
	if len(detail.Metrics) != 1 || detail.Metrics[0].NodeID != "orangepi" {
		t.Fatalf("metrics = %#v, want only orangepi", detail.Metrics)
	}
	if len(detail.Failures) != 1 || detail.Failures[0].ID != "orangepi-khoj" {
		t.Fatalf("failures = %#v, want only orangepi-khoj", detail.Failures)
	}
	if len(detail.Events) != 2 {
		t.Fatalf("events = %d, want 2 project-related events", len(detail.Events))
	}
}

func TestProjectDetailIncludesFilteredResourceStates(t *testing.T) {
	now := time.Date(2026, 6, 10, 9, 40, 0, 0, time.UTC)
	cfg := AppConfig{
		App: AppMeta{Name: "test"},
		Diagnostics: DiagnosticsConfig{ResourceThresholds: ResourceThresholdConfig{
			CPUPercent:     floatPtr(80),
			NetworkRXBps:   floatPtr(1000),
			StorageReadBps: floatPtr(2000),
		}},
	}
	summary := &SummaryResponse{
		GeneratedAt: now,
		Projects: []ProjectView{
			{ProjectConfig: ProjectConfig{ID: "knowledge-base", Name: "Knowledge Base"}, Status: StatusOK},
			{ProjectConfig: ProjectConfig{ID: "edge-dev", Name: "Edge Development"}, Status: StatusOK},
		},
		Nodes: []NodeView{
			{NodeConfig: NodeConfig{ID: "orangepi", Name: "orangepi"}, Status: StatusOK},
			{NodeConfig: NodeConfig{ID: "srv03", Name: "srv03"}, Status: StatusOK},
		},
		Services: []ServiceView{
			{ServiceConfig: ServiceConfig{ID: "orangepi-khoj", Name: "Khoj web", NodeID: "orangepi", ProjectID: "knowledge-base"}, Status: StatusOK},
			{ServiceConfig: ServiceConfig{ID: "srv03-ollama", Name: "Ollama", NodeID: "srv03", ProjectID: "edge-dev"}, Status: StatusOK},
		},
		Metrics: []MetricsView{
			{
				MetricsReport: MetricsReport{
					NodeID:  "orangepi",
					CPU:     CPUMetrics{Percent: 82},
					Memory:  MemoryMetrics{Percent: 50},
					Disk:    DiskMetrics{Percent: 40},
					Network: NetworkMetrics{RXBps: 1500},
				},
				Storage:   StorageMetrics{ReadBps: 1000},
				UpdatedAt: now,
			},
			{
				MetricsReport: MetricsReport{
					NodeID: "srv03",
					CPU:    CPUMetrics{Percent: 10},
					Memory: MemoryMetrics{Percent: 20},
					Disk:   DiskMetrics{Percent: 30},
				},
				UpdatedAt: now,
			},
		},
	}
	aggregator := NewAggregator(&cfg, nil, nil, time.Hour)
	aggregator.cached = summary
	aggregator.expiresAt = time.Now().Add(time.Hour)

	detail, err := aggregator.ProjectDetail(context.Background(), "knowledge-base")
	if err != nil {
		t.Fatalf("project detail: %v", err)
	}
	if len(detail.ResourceStates) != 1 || detail.ResourceStates[0].NodeID != "orangepi" {
		t.Fatalf("resource states = %#v, want only orangepi", detail.ResourceStates)
	}
	state := detail.ResourceStates[0]
	if state.Status != StatusDegraded || state.CPU.Headroom != -2 || state.NetworkRX.Headroom != -500 {
		t.Fatalf("resource state = %#v, want degraded cpu/network headroom", state)
	}
}

func TestNodeDetailFromSummaryFiltersRelatedData(t *testing.T) {
	now := time.Date(2026, 6, 10, 3, 0, 0, 0, time.UTC)
	summary := &SummaryResponse{
		GeneratedAt: now,
		Nodes: []NodeView{
			{NodeConfig: NodeConfig{ID: "orangepi", Name: "orangepi"}, Status: StatusDegraded},
			{NodeConfig: NodeConfig{ID: "srv03", Name: "srv03"}, Status: StatusOK},
		},
		Projects: []ProjectView{
			{ProjectConfig: ProjectConfig{ID: "knowledge-base", Name: "Knowledge Base"}, Status: StatusDegraded},
			{ProjectConfig: ProjectConfig{ID: "edge-dev", Name: "Edge Development"}, Status: StatusOK},
		},
		Services: []ServiceView{
			{ServiceConfig: ServiceConfig{ID: "orangepi-khoj", Name: "Khoj web", NodeID: "orangepi", ProjectID: "knowledge-base"}, Status: StatusDown},
			{ServiceConfig: ServiceConfig{ID: "orangepi-smb", Name: "SMB", NodeID: "orangepi", ProjectID: "knowledge-base"}, Status: StatusOK},
			{ServiceConfig: ServiceConfig{ID: "srv03-ollama", Name: "Ollama", NodeID: "srv03", ProjectID: "edge-dev"}, Status: StatusOK},
		},
		Metrics: []MetricsView{
			{MetricsReport: MetricsReport{NodeID: "orangepi", CPU: CPUMetrics{Percent: 7.5}}},
			{MetricsReport: MetricsReport{NodeID: "srv03", CPU: CPUMetrics{Percent: 12.5}}},
		},
		Events: []Event{
			{Kind: "node", SubjectID: "orangepi", Label: "orangepi", To: StatusDegraded, CreatedAt: now},
			{Kind: "service", SubjectID: "orangepi-khoj", Label: "Khoj web", To: StatusDown, CreatedAt: now},
			{Kind: "project", SubjectID: "knowledge-base", Label: "Knowledge Base", To: StatusDegraded, CreatedAt: now},
			{Kind: "service", SubjectID: "srv03-ollama", Label: "Ollama", To: StatusOK, CreatedAt: now},
		},
	}

	detail, err := nodeDetailFromSummary(summary, "orangepi")
	if err != nil {
		t.Fatalf("node detail: %v", err)
	}
	if detail.Node.ID != "orangepi" {
		t.Fatalf("node id = %s, want orangepi", detail.Node.ID)
	}
	if len(detail.Services) != 2 {
		t.Fatalf("services = %d, want 2 orangepi services", len(detail.Services))
	}
	if len(detail.Projects) != 1 || detail.Projects[0].ID != "knowledge-base" {
		t.Fatalf("projects = %#v, want knowledge-base", detail.Projects)
	}
	if detail.Metrics == nil || detail.Metrics.NodeID != "orangepi" {
		t.Fatalf("metrics = %#v, want orangepi", detail.Metrics)
	}
	if len(detail.Failures) != 1 || detail.Failures[0].ID != "orangepi-khoj" {
		t.Fatalf("failures = %#v, want only orangepi-khoj", detail.Failures)
	}
	if len(detail.Events) != 3 {
		t.Fatalf("events = %d, want node/service/project context events", len(detail.Events))
	}
}

func TestNodeDetailIncludesResourceState(t *testing.T) {
	now := time.Date(2026, 6, 10, 9, 50, 0, 0, time.UTC)
	cfg := AppConfig{
		App: AppMeta{Name: "test"},
		Diagnostics: DiagnosticsConfig{ResourceThresholds: ResourceThresholdConfig{
			MemoryPercent: floatPtr(70),
		}},
	}
	summary := &SummaryResponse{
		GeneratedAt: now,
		Nodes: []NodeView{
			{NodeConfig: NodeConfig{ID: "orangepi", Name: "orangepi"}, Status: StatusOK},
		},
		Projects: []ProjectView{
			{ProjectConfig: ProjectConfig{ID: "knowledge-base", Name: "Knowledge Base"}, Status: StatusOK},
		},
		Services: []ServiceView{
			{ServiceConfig: ServiceConfig{ID: "orangepi-khoj", Name: "Khoj web", NodeID: "orangepi", ProjectID: "knowledge-base"}, Status: StatusOK},
		},
		Metrics: []MetricsView{
			{
				MetricsReport: MetricsReport{
					NodeID: "orangepi",
					CPU:    CPUMetrics{Percent: 10},
					Memory: MemoryMetrics{Percent: 72},
					Disk:   DiskMetrics{Percent: 30},
				},
				UpdatedAt: now,
			},
		},
	}
	aggregator := NewAggregator(&cfg, nil, nil, time.Hour)
	aggregator.cached = summary
	aggregator.expiresAt = time.Now().Add(time.Hour)

	detail, err := aggregator.NodeDetail(context.Background(), "orangepi")
	if err != nil {
		t.Fatalf("node detail: %v", err)
	}
	if len(detail.ResourceStates) != 1 || detail.ResourceStates[0].NodeID != "orangepi" {
		t.Fatalf("resource states = %#v, want orangepi", detail.ResourceStates)
	}
	if detail.ResourceStates[0].Memory.Headroom != -2 || detail.ResourceStates[0].Status != StatusDegraded {
		t.Fatalf("resource state = %#v, want degraded memory headroom", detail.ResourceStates[0])
	}
}

func TestServiceDetailFromSummaryFiltersRelatedData(t *testing.T) {
	now := time.Date(2026, 6, 10, 2, 0, 0, 0, time.UTC)
	summary := &SummaryResponse{
		GeneratedAt: now,
		Projects: []ProjectView{
			{ProjectConfig: ProjectConfig{ID: "knowledge-base", Name: "Knowledge Base"}, Status: StatusDegraded},
			{ProjectConfig: ProjectConfig{ID: "edge-dev", Name: "Edge Development"}, Status: StatusOK},
		},
		Nodes: []NodeView{
			{NodeConfig: NodeConfig{ID: "orangepi", Name: "orangepi"}, Status: StatusDegraded},
			{NodeConfig: NodeConfig{ID: "srv03", Name: "srv03"}, Status: StatusOK},
		},
		Services: []ServiceView{
			{
				ServiceConfig: ServiceConfig{ID: "orangepi-khoj", Name: "Khoj web", NodeID: "orangepi", ProjectID: "knowledge-base", EndpointKey: "orangepi_khoj"},
				Status:        StatusDown,
				Detail:        "connection refused",
				LastCheckedAt: now,
			},
			{ServiceConfig: ServiceConfig{ID: "srv03-ollama", Name: "Ollama", NodeID: "srv03", ProjectID: "edge-dev"}, Status: StatusOK},
		},
		Metrics: []MetricsView{
			{MetricsReport: MetricsReport{NodeID: "orangepi", CPU: CPUMetrics{Percent: 7.5}}},
			{MetricsReport: MetricsReport{NodeID: "srv03", CPU: CPUMetrics{Percent: 12.5}}},
		},
		Events: []Event{
			{Kind: "service", SubjectID: "orangepi-khoj", Label: "Khoj web", To: StatusDown, CreatedAt: now},
			{Kind: "node", SubjectID: "orangepi", Label: "orangepi", To: StatusDegraded, CreatedAt: now},
			{Kind: "project", SubjectID: "knowledge-base", Label: "Knowledge Base", To: StatusDegraded, CreatedAt: now},
			{Kind: "service", SubjectID: "srv03-ollama", Label: "Ollama", To: StatusOK, CreatedAt: now},
		},
	}

	detail, err := serviceDetailFromSummary(summary, "orangepi-khoj")
	if err != nil {
		t.Fatalf("service detail: %v", err)
	}
	if detail.Service.ID != "orangepi-khoj" {
		t.Fatalf("service id = %s, want orangepi-khoj", detail.Service.ID)
	}
	if detail.Node == nil || detail.Node.ID != "orangepi" {
		t.Fatalf("node = %#v, want orangepi", detail.Node)
	}
	if detail.Project == nil || detail.Project.ID != "knowledge-base" {
		t.Fatalf("project = %#v, want knowledge-base", detail.Project)
	}
	if detail.Metrics == nil || detail.Metrics.NodeID != "orangepi" {
		t.Fatalf("metrics = %#v, want orangepi", detail.Metrics)
	}
	if detail.LatestCheck == nil || detail.LatestCheck.Key != "orangepi_khoj" || detail.LatestCheck.Status != StatusDown {
		t.Fatalf("latest check = %#v, want orangepi_khoj down", detail.LatestCheck)
	}
	if len(detail.Events) != 3 {
		t.Fatalf("events = %d, want service/node/project context events", len(detail.Events))
	}
}

func TestSaveMetricsV2StoresHistoryAndIORates(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	base := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	first := testMetricsReportV2("sh-core", base, 1000, 2000, 10_000, 20_000)
	first.Resources.Storage.Devices = []StorageDeviceMetric{{Name: "nvme0n1", ReadIOs: 100, WriteIOs: 200}}
	if _, _, err := store.SaveMetricsV2(context.Background(), first); err != nil {
		t.Fatalf("save first report: %v", err)
	}
	second := testMetricsReportV2("sh-core", base.Add(time.Minute), 7000, 8000, 70_000, 80_000)
	second.Resources.Storage.Devices = []StorageDeviceMetric{{Name: "nvme0n1", ReadIOs: 700, WriteIOs: 500}}
	if view, point, err := store.SaveMetricsV2(context.Background(), second); err != nil {
		t.Fatalf("save second report: %v", err)
	} else {
		if point.StorageReadBps != 100 || point.StorageWriteBps != 100 {
			t.Fatalf("storage bps = %v/%v, want 100/100", point.StorageReadBps, point.StorageWriteBps)
		}
		if point.StorageReadIOPS != 10 || point.StorageWriteIOPS != 5 {
			t.Fatalf("storage iops = %v/%v, want 10/5", point.StorageReadIOPS, point.StorageWriteIOPS)
		}
		if view.Storage.ReadIOPS != 10 || view.Storage.WriteIOPS != 5 {
			t.Fatalf("latest storage iops = %v/%v, want 10/5", view.Storage.ReadIOPS, view.Storage.WriteIOPS)
		}
	}

	points, err := store.MetricsHistory(context.Background(), "sh-core", base.Add(-time.Second), 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("history points = %d, want 2", len(points))
	}
	if points[1].NetworkRXBps != 1000 || points[1].NetworkTXBps != 1000 {
		t.Fatalf("network bps = %v/%v, want 1000/1000", points[1].NetworkRXBps, points[1].NetworkTXBps)
	}
	if points[1].StorageReadIOPS != 10 || points[1].StorageWriteIOPS != 5 {
		t.Fatalf("history storage iops = %v/%v, want 10/5", points[1].StorageReadIOPS, points[1].StorageWriteIOPS)
	}
}

func TestSaveMetricsV2PreservesLatestMetricDetails(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	report := testMetricsReportV2("srv03", time.Date(2026, 6, 10, 3, 0, 0, 0, time.UTC), 12_000, 18_000, 30_000, 60_000)
	report.Resources.CPU.PerCorePercent = []float64{10.5, 20.5, 30.5}
	report.Resources.CPU.ContextSwitches = 12345
	report.Resources.CPU.Interrupts = 67890
	report.Resources.Storage.Devices = []StorageDeviceMetric{
		{Name: "nvme0n1", ReadBytes: 12_000, WriteBytes: 18_000, ReadIOs: 12, WriteIOs: 18},
	}
	report.Resources.Network.Interfaces = []NetworkInterfaceMetric{
		{Name: "tailscale0", RXBytes: 30_000, TXBytes: 60_000, RXPackets: 300, TXPackets: 600, RXErrors: 1, TXDrops: 2},
	}
	report.Resources.GPU = GPUMetrics{
		Available: true,
		Provider:  "tegrastats",
		Devices: []GPUDeviceMetric{
			{Index: "0", Name: "Jetson GPU", UtilPercent: 42, MemoryTotal: 8 << 30, MemoryUsed: 2 << 30, MemoryPercent: 25, TemperatureC: 51},
		},
	}
	report.Resources.Containers = ContainerMetrics{
		Available: true,
		Provider:  "docker",
		Containers: []ContainerMetric{
			{
				ID:               "abc123",
				Name:             "statusd",
				Image:            "rtime/statusd:test",
				State:            "running",
				ComposeProject:   "rtime-status-board",
				CPUPercent:       2.5,
				MemoryPercent:    3.5,
				MemoryUsageBytes: 64 << 20,
				MemoryLimitBytes: 512 << 20,
				NetworkRXBytes:   1024,
				NetworkTXBytes:   2048,
				BlockReadBytes:   4096,
				BlockWriteBytes:  8192,
			},
		},
	}
	report.Resources.Processes = ProcessMetrics{
		ProcessCount: 42,
		Processes: []ProcessMetric{
			{PID: 1234, PPID: 1, User: "root", Command: "statusd", CPUPercent: 5.5, MemoryPercent: 1.5, RSSBytes: 128 << 20},
		},
	}
	report.CollectorStatus = []CollectorStatus{
		{Name: "cpu", OK: true, ElapsedMS: 251},
		{Name: "gpu", OK: false, Detail: "tegrastats timeout", ElapsedMS: 4001, Cached: true, CacheAgeSeconds: 61.5},
	}

	view, _, err := store.SaveMetricsV2(context.Background(), report)
	if err != nil {
		t.Fatalf("save report: %v", err)
	}
	if view.SchemaVersion != 2 || !view.GPU.Available || len(view.CollectorStatus) != 2 {
		t.Fatalf("returned view lost v2 detail: %#v", view)
	}

	latest, err := store.LatestMetrics(context.Background())
	if err != nil {
		t.Fatalf("latest metrics: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("latest metrics = %d, want 1", len(latest))
	}
	got := latest[0]
	if got.SchemaVersion != 2 {
		t.Fatalf("schema version = %d, want 2", got.SchemaVersion)
	}
	if len(got.CPU.PerCorePercent) != 3 || got.CPU.PerCorePercent[2] != 30.5 {
		t.Fatalf("per-core cpu = %#v, want preserved", got.CPU.PerCorePercent)
	}
	if got.CPU.ContextSwitches != 12345 || got.CPU.Interrupts != 67890 {
		t.Fatalf("cpu counters = %d/%d, want 12345/67890", got.CPU.ContextSwitches, got.CPU.Interrupts)
	}
	if len(got.Storage.Devices) != 1 || got.Storage.Devices[0].Name != "nvme0n1" {
		t.Fatalf("storage devices = %#v, want nvme0n1", got.Storage.Devices)
	}
	if len(got.Network.Interfaces) != 1 || got.Network.Interfaces[0].RXErrors != 1 || got.Network.Interfaces[0].TXDrops != 2 {
		t.Fatalf("network interfaces = %#v, want extended counters", got.Network.Interfaces)
	}
	if !got.GPU.Available || got.GPU.Provider != "tegrastats" || len(got.GPU.Devices) != 1 || got.GPU.Devices[0].UtilPercent != 42 {
		t.Fatalf("gpu = %#v, want Jetson metrics", got.GPU)
	}
	if !got.Containers.Available || got.Containers.Provider != "docker" || len(got.Containers.Containers) != 1 || got.Containers.Containers[0].ComposeProject != "rtime-status-board" {
		t.Fatalf("containers = %#v, want docker status-board container", got.Containers)
	}
	if got.Processes.ProcessCount != 42 || len(got.Processes.Processes) != 1 || got.Processes.Processes[0].Command != "statusd" {
		t.Fatalf("processes = %#v, want statusd top process", got.Processes)
	}
	if len(got.CollectorStatus) != 2 || got.CollectorStatus[1].OK || got.CollectorStatus[1].Detail != "tegrastats timeout" {
		t.Fatalf("collector status = %#v, want gpu timeout", got.CollectorStatus)
	}
	if !got.CollectorStatus[1].Cached || got.CollectorStatus[1].CacheAgeSeconds != 61.5 {
		t.Fatalf("collector cache metadata = %#v, want cached age 61.5", got.CollectorStatus[1])
	}

	logs, err := store.RecentMetricReports(context.Background(), "srv03", 10)
	if err != nil {
		t.Fatalf("recent metric reports: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("report logs = %d, want 1", len(logs))
	}
	if logs[0].NodeID != "srv03" || logs[0].SchemaVersion != 2 || logs[0].CollectorOK != 1 || logs[0].CollectorFailed != 1 {
		t.Fatalf("report log summary = %#v, want srv03 schema v2 with 1 ok and 1 failed collector", logs[0])
	}
	if !logs[0].GPUAvailable || logs[0].StorageDeviceCount != 1 || logs[0].NetworkInterfaceCount != 1 {
		t.Fatalf("report log resources = %#v, want gpu/storage/network counts", logs[0])
	}
}

func TestRecentMetricReportsFiltersAndCaps(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	store.reportLogLimit = 3

	base := time.Date(2026, 6, 10, 4, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		nodeID := "sh-core"
		if i%2 == 1 {
			nodeID = "srv03"
		}
		report := testMetricsReportV2(nodeID, base.Add(time.Duration(i)*time.Minute), int64(1000+i), int64(2000+i), int64(3000+i), int64(4000+i))
		report.CollectorStatus = []CollectorStatus{{Name: "cpu", OK: true}, {Name: "gpu", OK: i%2 == 1, Detail: "no gpu"}}
		if _, _, err := store.SaveMetricsV2(context.Background(), report); err != nil {
			t.Fatalf("save report %d: %v", i, err)
		}
	}

	allLogs, err := store.RecentMetricReports(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("recent reports: %v", err)
	}
	if len(allLogs) != 3 {
		t.Fatalf("all report logs = %d, want cap 3", len(allLogs))
	}
	if allLogs[0].CapturedAt.Before(allLogs[1].CapturedAt) {
		t.Fatalf("logs are not newest first: %#v", allLogs)
	}
	filtered, err := store.RecentMetricReports(context.Background(), "srv03", 10)
	if err != nil {
		t.Fatalf("filtered reports: %v", err)
	}
	if len(filtered) == 0 {
		t.Fatalf("filtered reports for srv03 are empty")
	}
	for _, item := range filtered {
		if item.NodeID != "srv03" {
			t.Fatalf("filtered log node = %s, want srv03", item.NodeID)
		}
	}
}

func TestMetricsReportsEndpointReturnsBoundedLogs(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	report := testMetricsReportV2("sh-core", time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC), 1000, 2000, 3000, 4000)
	report.CollectorStatus = []CollectorStatus{{Name: "cpu", OK: true}}
	if _, _, err := store.SaveMetricsV2(context.Background(), report); err != nil {
		t.Fatalf("save report: %v", err)
	}

	server := NewServer(ServerOptions{Store: store})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/reports?node_id=sh-core&limit=1", nil)
	recorder := httptest.NewRecorder()
	server.metricsReports(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response MetricsReportLogsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.NodeID != "sh-core" || response.Returned != 1 || len(response.Logs) != 1 {
		t.Fatalf("response = %#v, want one sh-core log", response)
	}
}

func TestRequestLoggingIncludesStatusAndBytes(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := NewServer(ServerOptions{Logger: logger})

	okRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(okRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if okRecorder.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", okRecorder.Code)
	}
	okLog := logs.String()
	if !strings.Contains(okLog, "status=200") || !strings.Contains(okLog, "bytes=") || !strings.Contains(okLog, "duration_ms=") {
		t.Fatalf("health log = %q, want status, bytes, and duration_ms", okLog)
	}

	logs.Reset()
	notFoundRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(notFoundRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/not-found", nil))
	if notFoundRecorder.Code != http.StatusNotFound {
		t.Fatalf("not-found status = %d, want 404", notFoundRecorder.Code)
	}
	notFoundLog := logs.String()
	if !strings.Contains(notFoundLog, "level=WARN") || !strings.Contains(notFoundLog, "status=404") || !strings.Contains(notFoundLog, "path=/api/v1/not-found") {
		t.Fatalf("not-found log = %q, want WARN status 404 path", notFoundLog)
	}

	stats := server.requestStats.Snapshot()
	if stats.Total != 2 || stats.StatusCounts.Success != 1 || stats.StatusCounts.ClientError != 1 {
		t.Fatalf("request stats = %#v, want one success and one client error", stats)
	}
	if len(stats.Routes) != 2 {
		t.Fatalf("routes = %#v, want health and unmatched routes", stats.Routes)
	}
	if stats.RecentP95DurationMS < 0 || stats.RecentMaxDurationMS < 0 {
		t.Fatalf("request latency stats = %#v, want non-negative values", stats)
	}
}

func TestRequestStatsNormalizesRoutesAndBoundsRecentSamples(t *testing.T) {
	stats := NewAPIRequestStats()
	stats.recentLimit = 2
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	stats.Record(http.MethodGet, "/api/v1/nodes/sh-core/metrics", http.StatusOK, 100, 25*time.Millisecond, now)
	stats.Record(http.MethodPost, "/api/v1/metrics/report/v2", http.StatusUnauthorized, 50, 600*time.Millisecond, now.Add(time.Second))
	stats.Record(http.MethodGet, "/api/v1/projects/rtime-fabric/checks", http.StatusBadGateway, 80, 10*time.Millisecond, now.Add(2*time.Second))

	snapshot := stats.Snapshot()
	if snapshot.Total != 3 || snapshot.SlowCount != 1 {
		t.Fatalf("snapshot totals = %#v, want total 3 and one slow request", snapshot)
	}
	if snapshot.StatusCounts.Success != 1 || snapshot.StatusCounts.ClientError != 1 || snapshot.StatusCounts.ServerError != 1 {
		t.Fatalf("status counts = %#v, want 2xx/4xx/5xx counts", snapshot.StatusCounts)
	}
	if len(snapshot.Recent) != 2 {
		t.Fatalf("recent = %d, want bounded 2", len(snapshot.Recent))
	}
	routes := map[string]bool{}
	for _, route := range snapshot.Routes {
		routes[route.Method+" "+route.Route] = true
	}
	for _, want := range []string{
		"GET /api/v1/nodes/:id/metrics",
		"POST /api/v1/metrics/report/v2",
		"GET /api/v1/projects/:id/checks",
	} {
		if !routes[want] {
			t.Fatalf("routes = %#v, missing %s", snapshot.Routes, want)
		}
	}
}

func TestAPIRequestIssuesAreAddedToOpsDigest(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	requests := APIRequestDiagnostic{
		Total:               3,
		SlowThresholdMS:     500,
		RecentP95DurationMS: 750,
		Recent: []APIRequestSample{
			{Method: http.MethodGet, Route: "/api/v1/summary", Status: http.StatusOK, DurationMS: 750, At: now},
			{Method: http.MethodGet, Route: "/api/v1/diagnostics", Status: http.StatusBadGateway, DurationMS: 20, At: now.Add(time.Second)},
		},
	}

	ops := opsWithAPIRequestIssues(OpsDiagnostic{}, requests, now)
	if ops.Counts.Error != 1 || ops.Counts.Warn != 1 {
		t.Fatalf("ops counts = %#v, want one error and one warn", ops.Counts)
	}
	ids := map[string]OpsIssue{}
	for _, issue := range ops.Issues {
		ids[issue.SubjectID] = issue
	}
	if ids["api-5xx"].Severity != "error" || ids["api-5xx"].Value != 1 {
		t.Fatalf("api-5xx issue = %#v, want one error", ids["api-5xx"])
	}
	if !strings.Contains(ids["api-5xx"].Detail, "GET /api/v1/diagnostics x1") {
		t.Fatalf("api-5xx detail = %q, want route summary", ids["api-5xx"].Detail)
	}
	if ids["api-slow"].Severity != "warn" || ids["api-slow"].Value != 750 || ids["api-slow"].Limit != 500 {
		t.Fatalf("api-slow issue = %#v, want latency warning with p95 and limit", ids["api-slow"])
	}
	if !strings.Contains(ids["api-slow"].Detail, "GET /api/v1/summary x1") {
		t.Fatalf("api-slow detail = %q, want route summary", ids["api-slow"].Detail)
	}
}

func TestAPIRequestIssuesIgnoreHealthyRecentSamples(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	requests := APIRequestDiagnostic{
		Total:           1,
		SlowThresholdMS: 500,
		Recent: []APIRequestSample{
			{Method: http.MethodGet, Route: "/api/v1/summary", Status: http.StatusOK, DurationMS: 20, At: now},
		},
	}

	ops := opsWithAPIRequestIssues(OpsDiagnostic{}, requests, now)
	if len(ops.Issues) != 0 || ops.Counts != (OpsIssueCounts{}) {
		t.Fatalf("ops = %#v, want no API request issues", ops)
	}
}

func TestSummarizeAPIRequestIssueRoutesRanksAndBoundsRoutes(t *testing.T) {
	samples := []APIRequestSample{
		{Method: http.MethodGet, Route: "/api/v1/summary", Status: http.StatusInternalServerError},
		{Method: http.MethodGet, Route: "/api/v1/summary", Status: http.StatusBadGateway},
		{Method: http.MethodGet, Route: "/api/v1/diagnostics", Status: http.StatusInternalServerError},
		{Method: http.MethodGet, Route: "/api/v1/nodes/:id", Status: http.StatusBadGateway},
		{Method: http.MethodGet, Route: "/api/v1/projects/:id", Status: http.StatusBadGateway},
	}
	summary := summarizeAPIRequestIssueRoutes(samples, func(sample APIRequestSample) bool { return sample.Status >= 500 })
	if summary != "GET /api/v1/summary x2, GET /api/v1/diagnostics x1, GET /api/v1/nodes/:id x1" {
		t.Fatalf("summary = %q, want top three ranked routes", summary)
	}
}

func TestStoreDiagnosticsReportsCountsAndRetention(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	store.SetMetricsRetention(14 * 24 * time.Hour)

	if err := store.RecordStatus(context.Background(), "service", "svc-a", "Service A", StatusOK, "ok"); err != nil {
		t.Fatalf("record initial status: %v", err)
	}
	if err := store.RecordStatus(context.Background(), "service", "svc-a", "Service A", StatusDown, "down"); err != nil {
		t.Fatalf("record changed status: %v", err)
	}
	report := testMetricsReportV2("sh-core", time.Date(2026, 6, 10, 5, 0, 0, 0, time.UTC), 1000, 2000, 3000, 4000)
	report.CollectorStatus = []CollectorStatus{{Name: "cpu", OK: true}}
	if _, _, err := store.SaveMetricsV2(context.Background(), report); err != nil {
		t.Fatalf("save report: %v", err)
	}

	diag, err := store.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("store diagnostics: %v", err)
	}
	if diag.DBSizeBytes <= 0 || diag.TotalSizeBytes < diag.DBSizeBytes {
		t.Fatalf("store sizes = %#v, want non-zero db and total >= db", diag)
	}
	if diag.StatusCacheRows != 1 || diag.EventRows != 1 || diag.MetricsLatestRows != 1 || diag.MetricsSampleRows != 1 || diag.MetricsReportLogRows != 1 {
		t.Fatalf("store row counts = %#v, want one row in each tracked table", diag)
	}
	if diag.LatestMetricAt == nil || diag.LatestReportAt == nil {
		t.Fatalf("latest times = %#v/%#v, want both populated", diag.LatestMetricAt, diag.LatestReportAt)
	}
	if diag.MetricsRetentionDays != 14 || diag.ReportLogRetentionDays != 7 || diag.ReportLogLimit != 2000 {
		t.Fatalf("retention = %#v, want metrics 14d report logs 7d cap 2000", diag)
	}
}

func TestDiagnosticsIncludesRuntimeStoreAndCache(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	report := testMetricsReportV2("node-a", time.Now().UTC().Add(-time.Second), 1000, 2000, 3000, 4000)
	report.CollectorStatus = []CollectorStatus{{Name: "cpu", OK: true}}
	if _, _, err := store.SaveMetricsV2(context.Background(), report); err != nil {
		t.Fatalf("save report: %v", err)
	}

	gatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/endpoints/statuses" {
			t.Fatalf("path = %s, want /api/v1/endpoints/statuses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`[
			{
				"name": "svc-a",
				"group": "node-a",
				"key": "svc_a_endpoint",
				"results": [
					{"duration": 10000000, "success": true, "timestamp": %q}
				]
			}
		]`, time.Now().UTC().Format(time.RFC3339Nano))))
	}))
	defer gatus.Close()

	cfg := AppConfig{
		App:   AppMeta{Name: "test"},
		Nodes: []NodeConfig{{ID: "node-a", Name: "Node A"}},
		Projects: []ProjectConfig{
			{ID: "project-a", Name: "Project A", ServiceIDs: []string{"svc-a"}},
		},
		Services: []ServiceConfig{
			{ID: "svc-a", Name: "Service A", NodeID: "node-a", ProjectID: "project-a", EndpointKey: "svc_a_endpoint"},
		},
	}
	aggregator := NewAggregator(&cfg, store, NewGatusClient(gatus.URL), 10*time.Second)
	if _, err := aggregator.Summary(context.Background()); err != nil {
		t.Fatalf("summary: %v", err)
	}
	diag, err := aggregator.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if diag.Runtime.GoVersion == "" || diag.Runtime.Goroutines <= 0 || diag.Runtime.Memory.SysBytes == 0 {
		t.Fatalf("runtime = %#v, want go version, goroutines, and memory", diag.Runtime)
	}
	if !diag.Runtime.SummaryCache.Cached || diag.Runtime.SummaryCache.TTLSeconds != 10 || diag.Runtime.SummaryCache.SecondsUntilExpiry <= 0 {
		t.Fatalf("summary cache = %#v, want warm cache with 10s ttl", diag.Runtime.SummaryCache)
	}
	if diag.Runtime.Store.MetricsLatestRows != 1 || diag.Runtime.Store.MetricsSampleRows != 1 || diag.Runtime.Store.MetricsReportLogRows != 1 {
		t.Fatalf("store diagnostics = %#v, want metrics rows", diag.Runtime.Store)
	}
}

func TestDiagnosticsIncludesEventLog(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.RecordStatus(context.Background(), "service", "svc-a", "Service A", StatusOK, "ok"); err != nil {
		t.Fatalf("record initial status: %v", err)
	}
	if err := store.RecordStatus(context.Background(), "service", "svc-a", "Service A", StatusDown, "connection refused"); err != nil {
		t.Fatalf("record changed status: %v", err)
	}

	gatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/endpoints/statuses" {
			t.Fatalf("path = %s, want /api/v1/endpoints/statuses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer gatus.Close()

	cfg := AppConfig{App: AppMeta{Name: "test"}}
	aggregator := NewAggregator(&cfg, store, NewGatusClient(gatus.URL), time.Minute)
	diag, err := aggregator.Diagnostics(context.Background())
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if diag.EventLog.Total != 1 || diag.EventLog.Returned != 1 || len(diag.EventLog.Events) != 1 {
		t.Fatalf("event log = %#v, want one returned event and one total row", diag.EventLog)
	}
	event := diag.EventLog.Events[0]
	if event.Kind != "service" || event.SubjectID != "svc-a" || event.From != StatusOK || event.To != StatusDown || event.Detail != "connection refused" {
		t.Fatalf("event = %#v, want service svc-a ok->down with detail", event)
	}
	if diag.EventLog.LatestAt == nil || !diag.EventLog.LatestAt.Equal(event.CreatedAt) {
		t.Fatalf("latest_at = %#v, want event created_at %s", diag.EventLog.LatestAt, event.CreatedAt)
	}
	if len(diag.EventLog.KindCounts) != 1 || diag.EventLog.KindCounts[0].Kind != "service" || diag.EventLog.KindCounts[0].Count != 1 {
		t.Fatalf("kind counts = %#v, want one service event", diag.EventLog.KindCounts)
	}
	if diag.EventLog.StatusCounts.Down != 1 || diag.EventLog.StatusCounts.OK != 0 {
		t.Fatalf("status counts = %#v, want one down transition target", diag.EventLog.StatusCounts)
	}
}

func TestAgentReportDiagnosticsSummarizesRecentCollectorHealth(t *testing.T) {
	base := time.Date(2026, 6, 10, 8, 6, 0, 0, time.UTC)
	reports := []MetricsReportLog{
		{
			ID:                    4,
			NodeID:                "node-a",
			Hostname:              "node-a-host",
			SchemaVersion:         2,
			CapturedAt:            base.Add(-10 * time.Second),
			ReceivedAt:            base,
			ReportLagSeconds:      10,
			CollectorOK:           3,
			CollectorFailed:       0,
			GPUAvailable:          false,
			StorageDeviceCount:    1,
			NetworkInterfaceCount: 2,
			CollectorStatus:       []CollectorStatus{{Name: "cpu", OK: true}},
		},
		{
			ID:               3,
			NodeID:           "node-a",
			SchemaVersion:    2,
			CapturedAt:       base.Add(-70 * time.Second),
			ReceivedAt:       base.Add(-time.Minute),
			CollectorOK:      2,
			CollectorFailed:  1,
			CollectorStatus:  []CollectorStatus{{Name: "gpu", OK: false, Detail: "nvidia-smi timeout", ElapsedMS: 2000}},
			GPUAvailable:     false,
			ReportLagSeconds: 70,
		},
		{
			ID:               2,
			NodeID:           "node-b",
			Hostname:         "node-b-host",
			SchemaVersion:    2,
			CapturedAt:       base.Add(-2 * time.Minute),
			ReceivedAt:       base.Add(-90 * time.Second),
			CollectorOK:      1,
			CollectorFailed:  2,
			CollectorStatus:  []CollectorStatus{{Name: "containers", OK: false, Detail: "docker unavailable", Cached: true, CacheAgeSeconds: 61}},
			GPUAvailable:     true,
			ReportLagSeconds: 30,
		},
	}

	diag := agentReportDiagnostics(reports, MetricsDiagnostic{
		ExpectedNodes: []string{"node-a", "node-b", "node-c"},
		MissingNodes:  []string{"node-c"},
		StaleNodes:    []string{"node-b"},
	})

	if len(diag) != 3 {
		t.Fatalf("agent diagnostics = %#v, want three expected nodes", diag)
	}
	byNode := map[string]AgentNodeDiagnostic{}
	for _, row := range diag {
		byNode[row.NodeID] = row
	}

	nodeA := byNode["node-a"]
	if nodeA.Status != StatusDegraded || nodeA.ReportCount != 2 || nodeA.FailedReportCount != 1 || nodeA.CollectorFailureCount != 1 {
		t.Fatalf("node-a summary = %#v, want degraded from recent historical collector failure", nodeA)
	}
	if nodeA.LatestCollectorFailed != 0 || nodeA.LatestReceivedAt == nil || !nodeA.LatestReceivedAt.Equal(base) {
		t.Fatalf("node-a latest = %#v, want newest clean report at %s", nodeA, base)
	}

	nodeB := byNode["node-b"]
	if nodeB.Status != StatusDegraded || nodeB.LatestCollectorFailed != 2 || len(nodeB.LatestFailedCollectors) != 1 {
		t.Fatalf("node-b summary = %#v, want latest collector failure", nodeB)
	}
	if !nodeB.GPUAvailable || !strings.Contains(nodeB.Detail, "stale") || !nodeB.LatestFailedCollectors[0].Cached {
		t.Fatalf("node-b detail = %#v, want stale GPU node with cached collector failure", nodeB)
	}

	nodeC := byNode["node-c"]
	if nodeC.Status != StatusUnknown || nodeC.ReportCount != 0 || !strings.Contains(nodeC.Detail, "no recent metrics report") {
		t.Fatalf("node-c summary = %#v, want unknown missing report", nodeC)
	}
}

func TestProjectChecksEndpointAggregatesRelatedServiceResults(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	gatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/endpoints/statuses" {
			t.Fatalf("path = %s, want /api/v1/endpoints/statuses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`[
			{
				"name": "config",
				"group": "node-a",
				"key": "svc_config_endpoint",
				"results": [
					{"duration": 10000000, "success": true, "timestamp": %q},
					{"duration": 15000000, "success": false, "errors": ["connection refused"], "timestamp": %q}
				]
			},
			{
				"name": "direct",
				"group": "node-b",
				"key": "svc_direct_endpoint",
				"results": [
					{"duration": 20000000, "success": true, "timestamp": %q}
				]
			},
			{
				"name": "other",
				"group": "node-c",
				"key": "svc_other_endpoint",
				"results": [
					{"duration": 30000000, "success": false, "errors": ["unrelated"], "timestamp": %q}
				]
			}
		]`,
			base.Format(time.RFC3339Nano),
			base.Add(time.Minute).Format(time.RFC3339Nano),
			base.Add(2*time.Minute).Format(time.RFC3339Nano),
			base.Add(3*time.Minute).Format(time.RFC3339Nano),
		)))
	}))
	defer gatus.Close()

	cfg := AppConfig{
		App: AppMeta{Name: "test"},
		Nodes: []NodeConfig{
			{ID: "node-a", Name: "node-a"},
			{ID: "node-b", Name: "node-b"},
			{ID: "node-c", Name: "node-c"},
		},
		Projects: []ProjectConfig{
			{ID: "project-a", Name: "Project A", ServiceIDs: []string{"svc-config"}},
			{ID: "other", Name: "Other", ServiceIDs: []string{"svc-other"}},
		},
		Services: []ServiceConfig{
			{ID: "svc-config", Name: "Config mapped", NodeID: "node-a", ProjectID: "other", EndpointKey: "svc_config_endpoint"},
			{ID: "svc-direct", Name: "Direct mapped", NodeID: "node-b", ProjectID: "project-a", EndpointKey: "svc_direct_endpoint"},
			{ID: "svc-other", Name: "Other", NodeID: "node-c", ProjectID: "other", EndpointKey: "svc_other_endpoint"},
		},
	}
	aggregator := NewAggregator(&cfg, store, NewGatusClient(gatus.URL), time.Hour)
	server := NewServer(ServerOptions{Config: &cfg, Store: store, Aggregator: aggregator})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-a/checks?window=24h&limit=2", nil)
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response ProjectCheckHistoryResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Project.ID != "project-a" || response.EndpointCount != 2 || response.Returned != 2 {
		t.Fatalf("response = %#v, want project-a with two endpoints and two returned rows", response)
	}
	if response.Summary.Total != 2 || response.Summary.Successes != 1 || response.Summary.Failures != 1 ||
		response.Summary.AvgResponseTimeMS != 17.5 || response.Summary.P95ResponseTimeMS != 20 || response.Summary.MaxResponseTimeMS != 20 ||
		response.Summary.LastFailureAt == nil || !response.Summary.LastFailureAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("summary = %#v, want one failure and latency stats from bounded project results", response.Summary)
	}
	if response.Results[0].ServiceID != "svc-direct" || response.Results[0].Status != StatusOK {
		t.Fatalf("first result = %#v, want newest direct service healthy", response.Results[0])
	}
	if response.Results[1].ServiceID != "svc-config" || response.Results[1].Status != StatusDown || response.Results[1].Detail != "connection refused" {
		t.Fatalf("second result = %#v, want config service failure", response.Results[1])
	}
	for _, result := range response.Results {
		if result.ServiceID == "svc-other" {
			t.Fatalf("unrelated service leaked into project checks: %#v", response.Results)
		}
	}
}

func TestNodeChecksEndpointAggregatesNodeServiceResults(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	gatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/endpoints/statuses" {
			t.Fatalf("path = %s, want /api/v1/endpoints/statuses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`[
			{
				"name": "node-a-web",
				"group": "node-a",
				"key": "svc_node_a_endpoint",
				"results": [
					{"duration": 10000000, "success": true, "timestamp": %q},
					{"duration": 15000000, "success": false, "errors": ["connection refused"], "timestamp": %q}
				]
			},
			{
				"name": "node-a-worker",
				"group": "node-a",
				"key": "svc_node_a_2_endpoint",
				"results": [
					{"duration": 20000000, "success": true, "timestamp": %q}
				]
			},
			{
				"name": "node-b-web",
				"group": "node-b",
				"key": "svc_node_b_endpoint",
				"results": [
					{"duration": 30000000, "success": false, "errors": ["unrelated"], "timestamp": %q}
				]
			}
		]`,
			base.Format(time.RFC3339Nano),
			base.Add(time.Minute).Format(time.RFC3339Nano),
			base.Add(2*time.Minute).Format(time.RFC3339Nano),
			base.Add(3*time.Minute).Format(time.RFC3339Nano),
		)))
	}))
	defer gatus.Close()

	cfg := AppConfig{
		App: AppMeta{Name: "test"},
		Nodes: []NodeConfig{
			{ID: "node-a", Name: "node-a"},
			{ID: "node-b", Name: "node-b"},
		},
		Projects: []ProjectConfig{
			{ID: "project-a", Name: "Project A", ServiceIDs: []string{"svc-a", "svc-a2"}},
			{ID: "project-b", Name: "Project B", ServiceIDs: []string{"svc-b"}},
		},
		Services: []ServiceConfig{
			{ID: "svc-a", Name: "Node A web", NodeID: "node-a", ProjectID: "project-a", EndpointKey: "svc_node_a_endpoint"},
			{ID: "svc-a2", Name: "Node A worker", NodeID: "node-a", ProjectID: "project-a", EndpointKey: "svc_node_a_2_endpoint"},
			{ID: "svc-b", Name: "Node B web", NodeID: "node-b", ProjectID: "project-b", EndpointKey: "svc_node_b_endpoint"},
		},
	}
	aggregator := NewAggregator(&cfg, store, NewGatusClient(gatus.URL), time.Hour)
	server := NewServer(ServerOptions{Config: &cfg, Store: store, Aggregator: aggregator})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-a/checks?window=24h&limit=2", nil)
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response NodeCheckHistoryResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Node.ID != "node-a" || response.EndpointCount != 2 || response.Returned != 2 {
		t.Fatalf("response = %#v, want node-a with two endpoints and two returned rows", response)
	}
	if response.Summary.Total != 2 || response.Summary.Successes != 1 || response.Summary.Failures != 1 ||
		response.Summary.AvgResponseTimeMS != 17.5 || response.Summary.P95ResponseTimeMS != 20 || response.Summary.MaxResponseTimeMS != 20 ||
		response.Summary.LastFailureAt == nil || !response.Summary.LastFailureAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("summary = %#v, want one failure and latency stats from bounded node results", response.Summary)
	}
	if response.Results[0].ServiceID != "svc-a2" || response.Results[0].Status != StatusOK {
		t.Fatalf("first result = %#v, want newest node-a worker healthy", response.Results[0])
	}
	if response.Results[1].ServiceID != "svc-a" || response.Results[1].Status != StatusDown || response.Results[1].Detail != "connection refused" {
		t.Fatalf("second result = %#v, want node-a web failure", response.Results[1])
	}
	for _, result := range response.Results {
		if result.ServiceID == "svc-b" {
			t.Fatalf("unrelated node service leaked into node checks: %#v", response.Results)
		}
	}
}

func TestServiceChecksEndpointReturnsSummary(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	gatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/endpoints/statuses" {
			t.Fatalf("path = %s, want /api/v1/endpoints/statuses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`[
			{
				"name": "service-a",
				"group": "node-a",
				"key": "svc_a_endpoint",
				"results": [
					{"duration": 10000000, "success": true, "timestamp": %q},
					{"duration": 20000000, "success": false, "errors": ["timeout"], "timestamp": %q},
					{"duration": 30000000, "success": true, "timestamp": %q}
				]
			}
		]`,
			base.Format(time.RFC3339Nano),
			base.Add(time.Minute).Format(time.RFC3339Nano),
			base.Add(2*time.Minute).Format(time.RFC3339Nano),
		)))
	}))
	defer gatus.Close()

	cfg := AppConfig{
		App:      AppMeta{Name: "test"},
		Nodes:    []NodeConfig{{ID: "node-a", Name: "node-a"}},
		Projects: []ProjectConfig{{ID: "project-a", Name: "Project A", ServiceIDs: []string{"svc-a"}}},
		Services: []ServiceConfig{{ID: "svc-a", Name: "Service A", NodeID: "node-a", ProjectID: "project-a", EndpointKey: "svc_a_endpoint"}},
	}
	aggregator := NewAggregator(&cfg, store, NewGatusClient(gatus.URL), time.Hour)
	server := NewServer(ServerOptions{Config: &cfg, Store: store, Aggregator: aggregator})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/services/svc-a/checks?window=24h&limit=3", nil)
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response ServiceCheckHistoryResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Service.ID != "svc-a" || response.EndpointKey != "svc_a_endpoint" || response.Returned != 3 {
		t.Fatalf("response = %#v, want service-a with three returned rows", response)
	}
	if response.Summary.Total != 3 || response.Summary.Successes != 2 || response.Summary.Failures != 1 ||
		response.Summary.AvgResponseTimeMS != 20 || response.Summary.P95ResponseTimeMS != 30 || response.Summary.MaxResponseTimeMS != 30 ||
		response.Summary.LastFailureAt == nil || !response.Summary.LastFailureAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("summary = %#v, want one failure and latency stats from service results", response.Summary)
	}
	if response.Results[0].Status != StatusOK || response.Results[0].ResponseTimeMS != 30 {
		t.Fatalf("first result = %#v, want newest healthy row", response.Results[0])
	}
	if response.Results[1].Status != StatusDown || response.Results[1].Detail != "timeout" {
		t.Fatalf("second result = %#v, want failure row", response.Results[1])
	}
}

func TestProjectMetricsHistoryAggregatesRelatedNodes(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	nodeAFirst := testMetricsReportV2("node-a", base, 1000, 1000, 10_000, 20_000)
	nodeAFirst.Resources.CPU.Percent = 11
	nodeAFirst.Resources.Memory.Percent = 40
	nodeAFirst.Resources.Disk.Percent = 30
	nodeAFirst.Resources.Storage.Devices = []StorageDeviceMetric{{Name: "nvme0n1", ReadIOs: 100, WriteIOs: 200}}
	nodeASecond := testMetricsReportV2("node-a", base.Add(time.Minute), 7000, 5000, 70_000, 80_000)
	nodeASecond.Resources.CPU.Percent = 33
	nodeASecond.Resources.Memory.Percent = 66
	nodeASecond.Resources.Disk.Percent = 44
	nodeASecond.Resources.Storage.Devices = []StorageDeviceMetric{{Name: "nvme0n1", ReadIOs: 700, WriteIOs: 500}}
	nodeB := testMetricsReportV2("node-b", base.Add(2*time.Minute), 3000, 4000, 30_000, 40_000)
	nodeB.Resources.CPU.Percent = 55
	nodeB.Resources.Memory.Percent = 77
	nodeB.Resources.Disk.Percent = 22
	nodeB.Resources.GPU = GPUMetrics{
		Available: true,
		Provider:  "nvidia-smi",
		Devices:   []GPUDeviceMetric{{Index: "0", Name: "test gpu", UtilPercent: 81}},
	}
	nodeC := testMetricsReportV2("node-c", base.Add(3*time.Minute), 9000, 9000, 90_000, 90_000)
	nodeC.Resources.CPU.Percent = 99
	for _, item := range []struct {
		name   string
		report MetricsReportV2
	}{
		{name: "node-a-first", report: nodeAFirst},
		{name: "node-a-second", report: nodeASecond},
		{name: "node-b", report: nodeB},
		{name: "node-c", report: nodeC},
	} {
		if _, _, err := store.SaveMetricsV2(context.Background(), item.report); err != nil {
			t.Fatalf("save %s: %v", item.name, err)
		}
	}

	cfg := AppConfig{
		App: AppMeta{Name: "test"},
		Nodes: []NodeConfig{
			{ID: "node-a", Name: "node-a"},
			{ID: "node-b", Name: "node-b"},
			{ID: "node-c", Name: "node-c"},
		},
		Projects: []ProjectConfig{
			{ID: "project-a", Name: "Project A", ServiceIDs: []string{"svc-config"}},
			{ID: "other", Name: "Other", ServiceIDs: []string{"svc-other"}},
		},
		Services: []ServiceConfig{
			{ID: "svc-config", Name: "Config mapped", NodeID: "node-a", ProjectID: "other"},
			{ID: "svc-direct", Name: "Direct mapped", NodeID: "node-b", ProjectID: "project-a"},
			{ID: "svc-other", Name: "Other", NodeID: "node-c", ProjectID: "other"},
		},
	}
	gatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/endpoints/statuses" {
			t.Fatalf("path = %s, want /api/v1/endpoints/statuses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer gatus.Close()
	aggregator := NewAggregator(&cfg, store, NewGatusClient(gatus.URL), 0)

	response, err := aggregator.ProjectMetricsHistory(context.Background(), "project-a", time.Hour, 10)
	if err != nil {
		t.Fatalf("project metrics history: %v", err)
	}
	if response.ProjectID != "project-a" || response.Returned != 3 {
		t.Fatalf("response summary = %#v, want project-a with 3 returned points", response)
	}
	if len(response.Nodes) != 2 {
		t.Fatalf("nodes = %#v, want node-a and node-b only", response.Nodes)
	}
	if response.Nodes[0].NodeID != "node-a" || response.Nodes[1].NodeID != "node-b" {
		t.Fatalf("node order = %#v, want sorted node-a/node-b", response.Nodes)
	}
	if response.Nodes[0].Summary.Samples != 2 || response.Nodes[0].Summary.MaxCPUPercent != 33 || response.Nodes[0].Summary.MaxMemoryPercent != 66 {
		t.Fatalf("node-a summary = %#v, want 2 samples and max cpu/memory", response.Nodes[0].Summary)
	}
	if response.Nodes[0].Summary.MaxStorageReadIOPS != 10 || response.Nodes[0].Summary.MaxStorageWriteIOPS != 5 {
		t.Fatalf("node-a iops summary = %#v, want read 10/write 5", response.Nodes[0].Summary)
	}
	if !response.Nodes[1].Summary.GPUAvailable || response.Nodes[1].Summary.MaxGPUPercent != 81 {
		t.Fatalf("node-b gpu summary = %#v, want gpu available at 81%%", response.Nodes[1].Summary)
	}

	server := NewServer(ServerOptions{Config: &cfg, Store: store, Aggregator: aggregator})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project-a/metrics?window=1h&limit=10", nil)
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var apiResponse ProjectMetricsHistoryResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &apiResponse); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if apiResponse.ProjectID != "project-a" || apiResponse.Returned != 3 || len(apiResponse.Nodes) != 2 {
		t.Fatalf("api response = %#v, want project-a with two related nodes", apiResponse)
	}

	nodeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-a/metrics?window=7d", nil)
	nodeRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(nodeRecorder, nodeRequest)
	if nodeRecorder.Code != http.StatusOK {
		t.Fatalf("node history status = %d, body = %s", nodeRecorder.Code, nodeRecorder.Body.String())
	}
	var nodeResponse MetricsHistoryResponse
	if err := json.Unmarshal(nodeRecorder.Body.Bytes(), &nodeResponse); err != nil {
		t.Fatalf("decode node history: %v", err)
	}
	if nodeResponse.NodeID != "node-a" || nodeResponse.Returned != 2 || nodeResponse.Summary.Samples != 2 {
		t.Fatalf("node history response = %#v, want node-a with summary samples", nodeResponse)
	}
	if nodeResponse.Summary.MaxCPUPercent != 33 || nodeResponse.Summary.MaxStorageReadIOPS != 10 || nodeResponse.Summary.MaxStorageWriteIOPS != 5 {
		t.Fatalf("node history summary = %#v, want max cpu/read iops/write iops", nodeResponse.Summary)
	}
}

func TestMetricsHistoryLimitKeepsNewestPoints(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	for i := 0; i < 4; i++ {
		report := testMetricsReportV2("node-a", base.Add(time.Duration(i)*time.Minute), int64(1000+i), int64(2000+i), int64(3000+i), int64(4000+i))
		report.Resources.CPU.Percent = float64(10 + i)
		if _, _, err := store.SaveMetricsV2(context.Background(), report); err != nil {
			t.Fatalf("save report %d: %v", i, err)
		}
	}
	points, err := store.MetricsHistory(context.Background(), "node-a", base.Add(-time.Minute), 2)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	if points[0].CPUPercent != 12 || points[1].CPUPercent != 13 {
		t.Fatalf("points cpu = %#v, want newest two points in ascending order", points)
	}
}

func TestSaveMetricsV2PrunesExpiredHistory(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "status-board.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	store.SetMetricsRetention(90 * time.Minute)

	base := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	oldReport := testMetricsReportV2("sh-core", base.Add(-2*time.Hour), 1000, 2000, 10_000, 20_000)
	if _, _, err := store.SaveMetricsV2(context.Background(), oldReport); err != nil {
		t.Fatalf("save old report: %v", err)
	}
	newReport := testMetricsReportV2("sh-core", base, 2000, 3000, 20_000, 30_000)
	if _, _, err := store.SaveMetricsV2(context.Background(), newReport); err != nil {
		t.Fatalf("save new report: %v", err)
	}

	points, err := store.MetricsHistory(context.Background(), "sh-core", base.Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("history points = %d, want 1 after pruning", len(points))
	}
	if !points[0].CapturedAt.Equal(base) {
		t.Fatalf("remaining point captured_at = %s, want %s", points[0].CapturedAt, base)
	}
}

func testMetricsReportV2(nodeID string, capturedAt time.Time, storageRead, storageWrite, networkRX, networkTX int64) MetricsReportV2 {
	return MetricsReportV2{
		SchemaVersion: 2,
		NodeID:        nodeID,
		Hostname:      nodeID,
		CapturedAt:    capturedAt,
		Resources: MetricsResources{
			CPU: CPUMetricsV2{CPUMetrics: CPUMetrics{Percent: 10, Load1: 1, Load5: 1, Load15: 1}},
			Memory: MemoryMetrics{
				TotalBytes: 100,
				UsedBytes:  50,
				Percent:    50,
			},
			Swap: MemoryMetrics{},
			Disk: DiskMetrics{Mountpoint: "/", TotalBytes: 100, UsedBytes: 40, Percent: 40},
			Storage: StorageMetrics{
				ReadBytes:  storageRead,
				WriteBytes: storageWrite,
			},
			Network: NetworkMetricsV2{
				RXBytes: networkRX,
				TXBytes: networkTX,
			},
			GPU:    GPUMetrics{Available: false, Provider: "none"},
			Uptime: UptimeMetrics{Seconds: 100},
		},
	}
}
