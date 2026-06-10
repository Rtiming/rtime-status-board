package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type GatusClient struct {
	baseURL string
	client  *http.Client
}

func NewGatusClient(baseURL string) *GatusClient {
	return &GatusClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 8 * time.Second},
	}
}

type gatusEndpoint struct {
	Name    string        `json:"name"`
	Group   string        `json:"group"`
	Key     string        `json:"key"`
	Results []gatusResult `json:"results"`
}

type gatusResult struct {
	Status           int                       `json:"status"`
	Hostname         string                    `json:"hostname"`
	Duration         int64                     `json:"duration"`
	Success          bool                      `json:"success"`
	Error            string                    `json:"error"`
	Errors           []string                  `json:"errors"`
	ConditionResults []EndpointConditionResult `json:"conditionResults"`
	Timestamp        time.Time                 `json:"timestamp"`
}

func (c *GatusClient) Statuses(ctx context.Context) (map[string]RuntimeCheck, error) {
	endpoints, err := c.EndpointStatuses(ctx)
	if err != nil {
		return nil, err
	}

	checks := make(map[string]RuntimeCheck, len(endpoints))
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
	return checks, nil
}

func (c *GatusClient) EndpointStatuses(ctx context.Context) ([]RuntimeEndpointStatus, error) {
	endpoints, err := c.endpoints(ctx)
	if err != nil {
		return nil, err
	}

	statuses := make([]RuntimeEndpointStatus, 0, len(endpoints))
	for _, endpoint := range endpoints {
		key := endpoint.normalizedKey()
		status := RuntimeEndpointStatus{
			Name:   endpoint.Name,
			Group:  endpoint.Group,
			Key:    key,
			Status: StatusUnknown,
			Detail: "No check result yet",
		}
		status.RecentResults = len(endpoint.Results)
		if len(endpoint.Results) > 0 {
			result := endpoint.Results[len(endpoint.Results)-1]
			status.Success = result.Success
			status.LastCheckedAt = result.Timestamp
			status.ResponseTimeMS = result.Duration / int64(time.Millisecond)
			status.Errors = result.errorMessages()
			status.Conditions = result.ConditionResults
			if result.Success {
				status.Status = StatusOK
				status.Detail = "Healthy"
			} else {
				status.Status = StatusDown
				status.Detail = result.detail()
			}
		}
		for _, result := range endpoint.Results {
			if !result.Success {
				status.RecentFailures++
			}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (c *GatusClient) EndpointResults(ctx context.Context, endpointKey string) ([]ServiceCheckResult, error) {
	endpoints, err := c.endpoints(ctx)
	if err != nil {
		return nil, err
	}
	for _, endpoint := range endpoints {
		if endpoint.normalizedKey() != endpointKey {
			continue
		}
		return endpoint.checkResults(), nil
	}
	return nil, fmt.Errorf("endpoint %s not found in Gatus", endpointKey)
}

func (c *GatusClient) EndpointResultsMap(ctx context.Context) (map[string][]ServiceCheckResult, error) {
	endpoints, err := c.endpoints(ctx)
	if err != nil {
		return nil, err
	}
	results := make(map[string][]ServiceCheckResult, len(endpoints))
	for _, endpoint := range endpoints {
		results[endpoint.normalizedKey()] = endpoint.checkResults()
	}
	return results, nil
}

func (c *GatusClient) endpoints(ctx context.Context) ([]gatusEndpoint, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/endpoints/statuses", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gatus returned %s", resp.Status)
	}

	var endpoints []gatusEndpoint
	if err := json.NewDecoder(resp.Body).Decode(&endpoints); err != nil {
		return nil, err
	}
	return endpoints, nil
}

func (e gatusEndpoint) normalizedKey() string {
	if e.Key != "" {
		return e.Key
	}
	return e.Group + "_" + e.Name
}

func (e gatusEndpoint) checkResults() []ServiceCheckResult {
	results := make([]ServiceCheckResult, 0, len(e.Results))
	for _, result := range e.Results {
		status := StatusDown
		detail := result.detail()
		if result.Success {
			status = StatusOK
			detail = "Healthy"
		}
		results = append(results, ServiceCheckResult{
			Timestamp:      result.Timestamp,
			Status:         status,
			Success:        result.Success,
			Detail:         detail,
			ResponseTimeMS: result.Duration / int64(time.Millisecond),
			Errors:         result.errorMessages(),
			Conditions:     result.ConditionResults,
		})
	}
	return results
}

func (r gatusResult) errorMessages() []string {
	if len(r.Errors) > 0 {
		return r.Errors
	}
	if r.Error != "" {
		return []string{r.Error}
	}
	return nil
}

func (r gatusResult) detail() string {
	if messages := r.errorMessages(); len(messages) > 0 {
		return strings.Join(messages, "; ")
	}
	failedConditions := []string{}
	for _, condition := range r.ConditionResults {
		if !condition.Success {
			failedConditions = append(failedConditions, condition.Condition)
		}
	}
	if len(failedConditions) > 0 {
		return "Failed conditions: " + strings.Join(failedConditions, "; ")
	}
	return "Latest check failed"
}
