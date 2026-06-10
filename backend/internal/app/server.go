package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ServerOptions struct {
	Config         *AppConfig
	Store          *Store
	Aggregator     *Aggregator
	FrontendDir    string
	HeartbeatToken string
	AgentToken     string
	Logger         *slog.Logger
}

type Server struct {
	options      ServerOptions
	requestStats *APIRequestStats
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func NewServer(options ServerOptions) *Server {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Server{options: options, requestStats: NewAPIRequestStats()}
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.health)
	mux.HandleFunc("/api/v1/summary", s.summary)
	mux.HandleFunc("/api/v1/nodes", s.nodes)
	mux.HandleFunc("/api/v1/nodes/", s.nodeSubresource)
	mux.HandleFunc("/api/v1/projects", s.projects)
	mux.HandleFunc("/api/v1/projects/", s.projectSubresource)
	mux.HandleFunc("/api/v1/services", s.services)
	mux.HandleFunc("/api/v1/services/", s.serviceSubresource)
	mux.HandleFunc("/api/v1/metrics", s.metrics)
	mux.HandleFunc("/api/v1/metrics/reports", s.metricsReports)
	mux.HandleFunc("/api/v1/metrics/history", s.metricsHistory)
	mux.HandleFunc("/api/v1/checks", s.checks)
	mux.HandleFunc("/api/v1/diagnostics", s.diagnostics)
	mux.HandleFunc("/api/v1/telemetry/schema", s.telemetrySchema)
	mux.HandleFunc("/api/v1/metrics/report/v2", s.metricsReportV2)
	mux.HandleFunc("/api/v1/metrics/report", s.metricsReport)
	mux.HandleFunc("/api/v1/events", s.events)
	mux.HandleFunc("/api/v1/heartbeats/", s.heartbeat)
	mux.HandleFunc("/", s.frontend)
	return s.withLogging(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now().UTC()})
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.options.Aggregator.Summary(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, summary)
}

func (s *Server) nodes(w http.ResponseWriter, r *http.Request) {
	summary, err := s.options.Aggregator.Summary(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, summary.Nodes)
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	summary, err := s.options.Aggregator.Summary(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, summary.Projects)
}

func (s *Server) services(w http.ResponseWriter, r *http.Request) {
	summary, err := s.options.Aggregator.Summary(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, summary.Services)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	events, err := s.options.Store.RecentEvents(r.Context(), 50)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, events)
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.options.Store.LatestMetrics(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) checks(w http.ResponseWriter, r *http.Request) {
	checks, err := s.options.Aggregator.Checks(r.Context())
	if err != nil {
		s.writeError(w, http.StatusBadGateway, err)
		return
	}
	s.writeJSON(w, http.StatusOK, checks)
}

func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	diagnostics, err := s.options.Aggregator.Diagnostics(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	requests := s.requestStats.Snapshot()
	diagnostics.Runtime.Requests = requests
	diagnostics.Ops = opsWithAPIRequestIssues(diagnostics.Ops, requests, time.Now().UTC())
	s.writeJSON(w, http.StatusOK, diagnostics)
}

func opsWithAPIRequestIssues(ops OpsDiagnostic, requests APIRequestDiagnostic, now time.Time) OpsDiagnostic {
	if requests.Total == 0 || len(requests.Recent) == 0 {
		return ops
	}

	var recentServerErrors int
	var recentSlow int
	var latest time.Time
	for _, sample := range requests.Recent {
		if sample.Status >= 500 {
			recentServerErrors++
		}
		if requests.SlowThresholdMS > 0 && sample.DurationMS >= requests.SlowThresholdMS {
			recentSlow++
		}
		if sample.At.After(latest) {
			latest = sample.At
		}
	}
	if latest.IsZero() {
		latest = now
	}

	if recentServerErrors > 0 {
		ops.Issues = append(ops.Issues, OpsIssue{
			Severity:   "error",
			Kind:       "runtime-api",
			SubjectID:  "api-5xx",
			Status:     StatusDown,
			Metric:     "http_5xx",
			Value:      float64(recentServerErrors),
			Unit:       "requests",
			Detail:     fmt.Sprintf("%d recent API samples returned 5xx", recentServerErrors),
			ObservedAt: latest,
		})
	}
	if recentSlow > 0 {
		ops.Issues = append(ops.Issues, OpsIssue{
			Severity:   "warn",
			Kind:       "runtime-api",
			SubjectID:  "api-slow",
			Status:     StatusDegraded,
			Metric:     "latency",
			Value:      requests.RecentP95DurationMS,
			Limit:      requests.SlowThresholdMS,
			Unit:       "ms",
			Detail:     fmt.Sprintf("%d recent API samples exceeded the slow request threshold", recentSlow),
			ObservedAt: latest,
		})
	}

	sort.Slice(ops.Issues, func(i, j int) bool {
		if severityRank(ops.Issues[i].Severity) == severityRank(ops.Issues[j].Severity) {
			if ops.Issues[i].Kind == ops.Issues[j].Kind {
				return ops.Issues[i].SubjectID < ops.Issues[j].SubjectID
			}
			return ops.Issues[i].Kind < ops.Issues[j].Kind
		}
		return severityRank(ops.Issues[i].Severity) < severityRank(ops.Issues[j].Severity)
	})
	ops.Counts = countOpsIssues(ops.Issues)
	return ops
}

func (s *Server) metricsReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if !s.validToken(r, s.options.AgentToken) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid agent token"))
		return
	}
	var report MetricsReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if report.NodeID == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("node_id is required"))
		return
	}
	view, err := s.options.Store.SaveMetrics(r.Context(), report)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "metrics": view})
}

func (s *Server) metricsReportV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if !s.validToken(r, s.options.AgentToken) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid agent token"))
		return
	}
	var report MetricsReportV2
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if report.NodeID == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("node_id is required"))
		return
	}
	view, point, err := s.options.Store.SaveMetricsV2(r.Context(), report)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "metrics": view, "sample": point})
}

func (s *Server) metricsHistory(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("node_id is required"))
		return
	}
	window := r.URL.Query().Get("window")
	response, err := s.historyResponse(r.Context(), nodeID, window)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) metricsReports(w http.ResponseWriter, r *http.Request) {
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	limit := parseLimit(r.URL.Query().Get("limit"), 30, 200)
	logs, err := s.options.Store.RecentMetricReports(r.Context(), nodeID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, MetricsReportLogsResponse{
		GeneratedAt: time.Now().UTC(),
		NodeID:      nodeID,
		Logs:        logs,
		Returned:    len(logs),
	})
}

func (s *Server) nodeSubresource(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		response, err := s.options.Aggregator.NodeDetail(r.Context(), parts[0])
		if err != nil {
			s.writeError(w, http.StatusNotFound, err)
			return
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "metrics" {
		response, err := s.historyResponse(r.Context(), parts[0], r.URL.Query().Get("window"))
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "checks" {
		window := r.URL.Query().Get("window")
		if window == "" {
			window = "24h"
		}
		duration, err := parseHistoryWindow(window)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		limit := parseLimit(r.URL.Query().Get("limit"), 60, 200)
		response, err := s.options.Aggregator.NodeChecks(r.Context(), parts[0], duration, limit)
		if err != nil {
			if strings.HasPrefix(err.Error(), "node ") {
				s.writeError(w, http.StatusNotFound, err)
				return
			}
			s.writeError(w, http.StatusBadGateway, err)
			return
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) projectSubresource(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/projects/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		response, err := s.options.Aggregator.ProjectDetail(r.Context(), parts[0])
		if err != nil {
			s.writeError(w, http.StatusNotFound, err)
			return
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "metrics" {
		duration, err := parseHistoryWindow(r.URL.Query().Get("window"))
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		limit := parseLimit(r.URL.Query().Get("limit"), 3000, 3000)
		response, err := s.options.Aggregator.ProjectMetricsHistory(r.Context(), parts[0], duration, limit)
		if err != nil {
			if strings.HasPrefix(err.Error(), "project ") {
				s.writeError(w, http.StatusNotFound, err)
				return
			}
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "checks" {
		window := r.URL.Query().Get("window")
		if window == "" {
			window = "24h"
		}
		duration, err := parseHistoryWindow(window)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		limit := parseLimit(r.URL.Query().Get("limit"), 60, 200)
		response, err := s.options.Aggregator.ProjectChecks(r.Context(), parts[0], duration, limit)
		if err != nil {
			if strings.HasPrefix(err.Error(), "project ") {
				s.writeError(w, http.StatusNotFound, err)
				return
			}
			s.writeError(w, http.StatusBadGateway, err)
			return
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) serviceSubresource(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/services/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		response, err := s.options.Aggregator.ServiceDetail(r.Context(), parts[0])
		if err != nil {
			s.writeError(w, http.StatusNotFound, err)
			return
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "checks" {
		window := r.URL.Query().Get("window")
		if window == "" {
			window = "24h"
		}
		duration, err := parseHistoryWindow(window)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		limit := parseLimit(r.URL.Query().Get("limit"), 30, 100)
		response, err := s.options.Aggregator.ServiceChecks(r.Context(), parts[0], duration, limit)
		if err != nil {
			if strings.HasPrefix(err.Error(), "service ") {
				s.writeError(w, http.StatusNotFound, err)
				return
			}
			s.writeError(w, http.StatusBadGateway, err)
			return
		}
		s.writeJSON(w, http.StatusOK, response)
		return
	}
	http.NotFound(w, r)
}

func parseLimit(raw string, fallback, max int) int {
	if fallback <= 0 {
		fallback = 30
	}
	if max <= 0 {
		max = fallback
	}
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func (s *Server) historyResponse(ctx context.Context, nodeID, window string) (MetricsHistoryResponse, error) {
	duration, err := parseHistoryWindow(window)
	if err != nil {
		return MetricsHistoryResponse{}, err
	}
	points, err := s.options.Store.MetricsHistory(ctx, nodeID, time.Now().UTC().Add(-duration), 3000)
	if err != nil {
		return MetricsHistoryResponse{}, err
	}
	return MetricsHistoryResponse{
		NodeID:   nodeID,
		Window:   duration.String(),
		Summary:  summarizeMetricsHistory(points),
		Points:   points,
		Returned: len(points),
	}, nil
}

func parseHistoryWindow(window string) (time.Duration, error) {
	if window == "" {
		return time.Hour, nil
	}
	normalized := strings.TrimSpace(strings.ToLower(window))
	var duration time.Duration
	if strings.HasSuffix(normalized, "d") {
		days, err := time.ParseDuration(strings.TrimSuffix(normalized, "d") + "h")
		if err != nil {
			return 0, err
		}
		duration = days * 24
	} else {
		parsed, err := time.ParseDuration(normalized)
		if err != nil {
			return 0, err
		}
		duration = parsed
	}
	if duration <= 0 {
		return time.Hour, nil
	}
	if duration > 30*24*time.Hour {
		return 30 * 24 * time.Hour, nil
	}
	return duration, nil
}

func (s *Server) telemetrySchema(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, TelemetrySchemaResponse{
		Version: 2,
		Domains: []TelemetryDomain{
			{ID: "cpu", Label: "CPU", Fields: []string{"total_percent", "per_core_percent", "load_1_5_15", "context_switches", "interrupts"}},
			{ID: "memory", Label: "Memory", Fields: []string{"total", "used", "available", "swap"}},
			{ID: "disk", Label: "Disk Usage", Fields: []string{"mountpoint", "total", "used", "percent"}},
			{ID: "storage", Label: "Storage IO", Fields: []string{"read_bytes_per_sec", "write_bytes_per_sec", "read_iops", "write_iops"}},
			{ID: "network", Label: "Network", Fields: []string{"rx_bytes_per_sec", "tx_bytes_per_sec", "packets", "errors", "drops"}},
			{ID: "gpu", Label: "GPU", Optional: true, Fields: []string{"utilization", "memory", "temperature", "power"}},
			{ID: "containers", Label: "Containers", Optional: true, Fields: []string{"cpu", "memory", "state"}},
			{ID: "processes", Label: "Processes", Optional: true, Fields: []string{"top_cpu", "top_memory"}},
		},
	})
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if !s.validToken(r, s.options.HeartbeatToken) {
		s.writeError(w, http.StatusUnauthorized, errors.New("invalid heartbeat token"))
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/heartbeats/")
	id = strings.Trim(id, "/")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, errors.New("heartbeat id is required"))
		return
	}

	var payload struct {
		Status Status `json:"status"`
		Detail string `json:"detail"`
		Label  string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	if payload.Status == "" {
		payload.Status = StatusOK
	}
	if payload.Label == "" {
		payload.Label = id
	}
	if err := s.options.Store.RecordStatus(context.Background(), "heartbeat", id, payload.Label, payload.Status, payload.Detail); err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "status": payload.Status})
}

func (s *Server) validToken(r *http.Request, token string) bool {
	if token == "" || token == "change-me" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+token
}

func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if s.options.FrontendDir == "" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.options.FrontendDir, filepath.Clean(r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.options.FrontendDir, "index.html"))
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	s.writeJSON(w, status, map[string]any{"error": err.Error()})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		recorder := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(start)
		s.requestStats.Record(r.Method, r.URL.Path, status, recorder.bytes, duration, time.Now().UTC())
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		s.options.Logger.Log(
			r.Context(),
			level,
			"request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"bytes", recorder.bytes,
			"duration_ms", float64(duration.Microseconds())/1000,
		)
	})
}
