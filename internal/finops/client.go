package finops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"
	"watchdog-agent/internal/config"
)

// DeploymentCost represents the cost of a deployment.
type DeploymentCost struct {
	TotalCost float64
	CPUCost   float64
	RAMCost   float64
	GPUCost   float64
}

// NamespaceCost represents the cost of an entire namespace.
type NamespaceCost struct {
	TotalCost float64
	CPUCost   float64
	RAMCost   float64
	GPUCost   float64
}

// ClusterCost represents the aggregated cost for the cluster.
type ClusterCost struct {
	TotalCost float64
	CPUCost   float64
	RAMCost   float64
	GPUCost   float64
}

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

// openCostResponse represents the structure of the OpenCost API response.
type openCostResponse struct {
	Code int `json:"code"`
	Data []map[string]struct {
		TotalCost float64 `json:"totalCost"`
		CPUCost   float64 `json:"cpuCost"`
		RAMCost   float64 `json:"ramCost"`
		GPUCost   float64 `json:"gpuCost"`
	} `json:"data"`
}

// executeQuery makes an HTTP GET request to the OpenCost API and aggregates the result.
func (c *Client) executeQuery(ctx context.Context, window, aggregate, filter string) (float64, float64, float64, float64, error) {
	reqURL, err := url.Parse(c.config.OpenCost.URL + "/allocation/compute")
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid opencost url: %w", err)
	}

	q := reqURL.Query()
	q.Set("window", window)
	q.Set("aggregate", aggregate)
	if filter != "" {
		q.Set("filter", filter)
	}
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("opencost request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, 0, 0, fmt.Errorf("opencost returned status: %d", resp.StatusCode)
	}

	var result openCostResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to decode opencost response: %w", err)
	}

	if result.Code != 200 {
		return 0, 0, 0, 0, fmt.Errorf("opencost API error code: %d", result.Code)
	}

	if len(result.Data) == 0 || len(result.Data[0]) == 0 {
		slog.Warn("OpenCost returned empty data", slog.String("query", reqURL.String()))
		return 0, 0, 0, 0, nil
	}

	var total, cpu, ram, gpu float64
	// Aggregate across all keys in the data[0] map
	for _, item := range result.Data[0] {
		total += item.TotalCost
		cpu += item.CPUCost
		ram += item.RAMCost
		gpu += item.GPUCost
	}

	return total, cpu, ram, gpu, nil
}

// GetNamespaceCost aggregates cost for an entire namespace.
func (c *Client) GetNamespaceCost(ctx context.Context, namespace, window string) (*NamespaceCost, error) {
	filter := fmt.Sprintf(`namespace:"%s"`, namespace)
	total, cpu, ram, gpu, err := c.executeQuery(ctx, window, "namespace", filter)
	if err != nil {
		return nil, err
	}
	return &NamespaceCost{TotalCost: total, CPUCost: cpu, RAMCost: ram, GPUCost: gpu}, nil
}

// GetClusterCost aggregates cost for the whole cluster.
func (c *Client) GetClusterCost(ctx context.Context, window string) (*ClusterCost, error) {
	total, cpu, ram, gpu, err := c.executeQuery(ctx, window, "cluster", "")
	if err != nil {
		return nil, err
	}
	return &ClusterCost{TotalCost: total, CPUCost: cpu, RAMCost: ram, GPUCost: gpu}, nil
}

// GetDeploymentCost aggregates cost for a specific deployment.
func (c *Client) GetDeploymentCost(ctx context.Context, namespace, deployment, window string) (*DeploymentCost, error) {
	filter := fmt.Sprintf(`namespace:"%s"`, namespace)
	// OpenCost allows filtering by controller, but it depends on the exact labels.
	// We will query by namespace and filter the result map in `executeQuery` but wait,
	// if we aggregate by controller it returns the deployment cost.
	total, cpu, ram, gpu, err := c.executeQuery(ctx, window, "controller", filter)
	if err != nil {
		return nil, err
	}
	// Note: The execution above aggregates ALL controllers in the namespace since our filter
	// is just the namespace. We need to filter exactly to the deployment.
	// To fix this without complex filters, we will add controller filtering:
	// OpenCost supports `controller:"name"`.
	filter = fmt.Sprintf(`namespace:"%s"+controller:"%s"`, namespace, deployment)
	total, cpu, ram, gpu, err = c.executeQuery(ctx, window, "controller", filter)
	if err != nil {
		return nil, err
	}
	return &DeploymentCost{TotalCost: total, CPUCost: cpu, RAMCost: ram, GPUCost: gpu}, nil
}
