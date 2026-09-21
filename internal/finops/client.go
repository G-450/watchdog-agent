package finops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"watchdog-agent/internal/config"
)

// minutesPerMonth matches OpenCost's 730-hour month.
const minutesPerMonth = 730 * 60

// Client handles communication with the OpenCost API for real-time pricing data.
type Client struct {
	config     *config.Config
	httpClient *http.Client
}

// NewClient initializes a new FinOps client.
func NewClient(cfg *config.Config) *Client {
	timeout, err := time.ParseDuration(cfg.OpenCost.Timeout)
	if err != nil {
		timeout = 10 * time.Second
	}
	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Allocations holds monthly run-rate costs (USD) per pod, keyed by namespace then pod name.
type Allocations struct {
	pods map[string]map[string]float64
}

// ClusterCost returns the monthly run-rate of every allocation, including idle and unallocated cost.
func (a *Allocations) ClusterCost() float64 {
	var total float64
	for _, pods := range a.pods {
		for _, cost := range pods {
			total += cost
		}
	}
	return total
}

// NamespaceCost returns the monthly run-rate of all pods in a namespace.
func (a *Allocations) NamespaceCost(namespace string) float64 {
	var total float64
	for _, cost := range a.pods[namespace] {
		total += cost
	}
	return total
}

// WorkloadCost returns the monthly run-rate of the pods in a namespace accepted by owns.
func (a *Allocations) WorkloadCost(namespace string, owns func(pod string) bool) float64 {
	var total float64
	for pod, cost := range a.pods[namespace] {
		if owns(pod) {
			total += cost
		}
	}
	return total
}

type allocation struct {
	TotalCost  float64 `json:"totalCost"`
	Minutes    float64 `json:"minutes"`
	Properties struct {
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
	} `json:"properties"`
}

type allocationResponse struct {
	Code    int                     `json:"code"`
	Message string                  `json:"message"`
	Data    []map[string]allocation `json:"data"`
}

// GetAllocations fetches pod-level cost over the configured window in a single request.
// OpenCost ignores allocation filters in the deployed version, so namespace and workload
// costs are derived from the pod breakdown instead of separate filtered queries.
func (c *Client) GetAllocations(ctx context.Context) (*Allocations, error) {
	reqURL, err := url.Parse(strings.TrimRight(c.config.OpenCost.URL, "/") + "/allocation/compute")
	if err != nil {
		return nil, fmt.Errorf("invalid opencost url: %w", err)
	}
	q := reqURL.Query()
	q.Set("window", c.config.OpenCost.Window)
	q.Set("aggregate", "namespace,pod")
	q.Set("accumulate", "true")
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencost request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencost returned status: %d", resp.StatusCode)
	}
	var result allocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode opencost response: %w", err)
	}
	if result.Code != http.StatusOK {
		return nil, fmt.Errorf("opencost API error code %d: %s", result.Code, result.Message)
	}

	allocations := &Allocations{pods: make(map[string]map[string]float64)}
	for _, set := range result.Data {
		for key, item := range set {
			namespace, pod := item.Properties.Namespace, item.Properties.Pod
			if namespace == "" || pod == "" {
				// Idle and unallocated entries carry no pod; keep them in the cluster total only.
				namespace, pod = "", key
			}
			if item.Minutes <= 0 {
				continue
			}
			if allocations.pods[namespace] == nil {
				allocations.pods[namespace] = make(map[string]float64)
			}
			allocations.pods[namespace][pod] += item.TotalCost / item.Minutes * minutesPerMonth
		}
	}
	if len(allocations.pods) == 0 {
		slog.Warn("OpenCost returned no allocations", slog.String("window", c.config.OpenCost.Window))
	}
	return allocations, nil
}
