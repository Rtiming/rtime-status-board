package app

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultAPIRequestRecentLimit    = 80
	defaultAPISlowRequestThreshold  = 500 * time.Millisecond
	defaultAPIRequestUnknownPattern = "/api/unmatched"
)

type APIRequestStats struct {
	mu            sync.Mutex
	since         time.Time
	recentLimit   int
	slowThreshold time.Duration

	total         int64
	slowCount     int64
	totalDuration time.Duration
	maxDuration   time.Duration
	statusCounts  APIRequestStatusCounts
	routes        map[string]*apiRouteStats
	recent        []APIRequestSample
}

type apiRouteStats struct {
	Method        string
	Route         string
	Total         int64
	SlowCount     int64
	TotalDuration time.Duration
	MaxDuration   time.Duration
	LastStatus    int
	LastDuration  time.Duration
	LastSeenAt    time.Time
	StatusCounts  APIRequestStatusCounts
}

func NewAPIRequestStats() *APIRequestStats {
	return &APIRequestStats{
		since:         time.Now().UTC(),
		recentLimit:   defaultAPIRequestRecentLimit,
		slowThreshold: defaultAPISlowRequestThreshold,
		routes:        map[string]*apiRouteStats{},
		recent:        []APIRequestSample{},
	}
}

func (s *APIRequestStats) Record(method, path string, status int, bytes int, duration time.Duration, at time.Time) {
	if s == nil {
		return
	}
	if status == 0 {
		status = 200
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	route := normalizeAPIPath(path)
	durationMS := duration.Seconds() * 1000
	if durationMS < 0 {
		durationMS = 0
	}
	if bytes < 0 {
		bytes = 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.total++
	s.totalDuration += duration
	if duration > s.maxDuration {
		s.maxDuration = duration
	}
	if duration >= s.slowThreshold {
		s.slowCount++
	}
	incrementStatusCount(&s.statusCounts, status)

	key := method + " " + route
	routeStats := s.routes[key]
	if routeStats == nil {
		routeStats = &apiRouteStats{Method: method, Route: route}
		s.routes[key] = routeStats
	}
	routeStats.Total++
	routeStats.TotalDuration += duration
	if duration > routeStats.MaxDuration {
		routeStats.MaxDuration = duration
	}
	if duration >= s.slowThreshold {
		routeStats.SlowCount++
	}
	routeStats.LastStatus = status
	routeStats.LastDuration = duration
	routeStats.LastSeenAt = at
	incrementStatusCount(&routeStats.StatusCounts, status)

	sample := APIRequestSample{
		Method:     method,
		Route:      route,
		Status:     status,
		Bytes:      int64(bytes),
		DurationMS: durationMS,
		At:         at,
	}
	s.recent = append(s.recent, sample)
	if len(s.recent) > s.recentLimit {
		copy(s.recent, s.recent[len(s.recent)-s.recentLimit:])
		s.recent = s.recent[:s.recentLimit]
	}
}

func (s *APIRequestStats) Snapshot() APIRequestDiagnostic {
	if s == nil {
		return APIRequestDiagnostic{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	recent := make([]APIRequestSample, len(s.recent))
	copy(recent, s.recent)
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].At.After(recent[j].At)
	})

	routes := make([]APIRequestRouteDiagnostic, 0, len(s.routes))
	for _, routeStats := range s.routes {
		avgMS := float64(0)
		if routeStats.Total > 0 {
			avgMS = routeStats.TotalDuration.Seconds() * 1000 / float64(routeStats.Total)
		}
		routes = append(routes, APIRequestRouteDiagnostic{
			Method:         routeStats.Method,
			Route:          routeStats.Route,
			Total:          routeStats.Total,
			StatusCounts:   routeStats.StatusCounts,
			AvgDurationMS:  avgMS,
			MaxDurationMS:  routeStats.MaxDuration.Seconds() * 1000,
			SlowCount:      routeStats.SlowCount,
			LastStatus:     routeStats.LastStatus,
			LastDurationMS: routeStats.LastDuration.Seconds() * 1000,
			LastSeenAt:     routeStats.LastSeenAt,
		})
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Total == routes[j].Total {
			if routes[i].Route == routes[j].Route {
				return routes[i].Method < routes[j].Method
			}
			return routes[i].Route < routes[j].Route
		}
		return routes[i].Total > routes[j].Total
	})

	avgMS := float64(0)
	if s.total > 0 {
		avgMS = s.totalDuration.Seconds() * 1000 / float64(s.total)
	}
	return APIRequestDiagnostic{
		Since:               s.since,
		Total:               s.total,
		StatusCounts:        s.statusCounts,
		SlowThresholdMS:     s.slowThreshold.Seconds() * 1000,
		SlowCount:           s.slowCount,
		AvgDurationMS:       avgMS,
		MaxDurationMS:       s.maxDuration.Seconds() * 1000,
		RecentSampleLimit:   s.recentLimit,
		RecentP95DurationMS: percentileRecentDuration(recent, 0.95),
		RecentMaxDurationMS: maxRecentDuration(recent),
		Recent:              recent,
		Routes:              routes,
	}
}

func normalizeAPIPath(path string) string {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return defaultAPIRequestUnknownPattern
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "v1" {
		return defaultAPIRequestUnknownPattern
	}
	base := "/api/v1/" + parts[2]
	if len(parts) == 3 {
		switch parts[2] {
		case "health", "summary", "nodes", "projects", "services", "metrics", "checks", "diagnostics", "events":
			return base
		default:
			return defaultAPIRequestUnknownPattern
		}
	}
	switch parts[2] {
	case "nodes":
		if len(parts) == 4 {
			return "/api/v1/nodes/:id"
		}
		if len(parts) == 5 && (parts[4] == "metrics" || parts[4] == "checks") {
			return "/api/v1/nodes/:id/" + parts[4]
		}
	case "projects":
		if len(parts) == 4 {
			return "/api/v1/projects/:id"
		}
		if len(parts) == 5 && (parts[4] == "metrics" || parts[4] == "checks") {
			return "/api/v1/projects/:id/" + parts[4]
		}
	case "services":
		if len(parts) == 4 {
			return "/api/v1/services/:id"
		}
		if len(parts) == 5 && parts[4] == "checks" {
			return "/api/v1/services/:id/checks"
		}
	case "metrics":
		if len(parts) == 4 && (parts[3] == "reports" || parts[3] == "history" || parts[3] == "report") {
			return "/api/v1/metrics/" + parts[3]
		}
		if len(parts) == 5 && parts[3] == "report" && parts[4] == "v2" {
			return "/api/v1/metrics/report/v2"
		}
	case "telemetry":
		if len(parts) == 4 && parts[3] == "schema" {
			return "/api/v1/telemetry/schema"
		}
	case "heartbeats":
		if len(parts) == 4 {
			return "/api/v1/heartbeats/:id"
		}
	}
	return defaultAPIRequestUnknownPattern
}

func incrementStatusCount(counts *APIRequestStatusCounts, status int) {
	switch {
	case status >= 500:
		counts.ServerError++
	case status >= 400:
		counts.ClientError++
	case status >= 300:
		counts.Redirect++
	case status >= 200:
		counts.Success++
	default:
		counts.Other++
	}
}

func percentileRecentDuration(samples []APIRequestSample, percentile float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	values := make([]float64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.DurationMS)
	}
	sort.Float64s(values)
	index := int(math.Ceil(float64(len(values))*percentile)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func maxRecentDuration(samples []APIRequestSample) float64 {
	maximum := float64(0)
	for _, sample := range samples {
		if sample.DurationMS > maximum {
			maximum = sample.DurationMS
		}
	}
	return maximum
}
