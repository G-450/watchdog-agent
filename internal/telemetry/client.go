package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"time"

	"watchdog-agent/internal/config"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// Client handles communication with the Prometheus API to fetch resource usage metrics.
type Client struct {
	v1api  v1.API
	config *config.Config
}

// NewClient initializes a new Telemetry client connecting to the given Prometheus URL.
func NewClient(cfg *config.Config) (*Client, error) {
	client, err := api.NewClient(api.Config{
		Address: cfg.Prometheus.URL,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating prometheus client: %w", err)
	}

	v1api := v1.NewAPI(client)
	return &Client{v1api: v1api, config: cfg}, nil
}

// executeQuery is a helper to run PromQL and return a float64
func (c *Client) executeQuery(ctx context.Context, query string) (float64, error) {
	timeout, err := time.ParseDuration(c.config.Prometheus.Timeout)
	if err != nil {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, warnings, err := c.v1api.Query(ctx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("prometheus query failed: %w", err)
	}
	if len(warnings) > 0 {
		slog.Warn("Prometheus query returned warnings", slog.Any("warnings", warnings), slog.String("query", query))
	}

	vec, ok := result.(model.Vector)
	if !ok || len(vec) == 0 {
		return 0, nil // No data
	}

	val, err := strconv.ParseFloat(vec[0].Value.String(), 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse prometheus value: %w", err)
	}

	return val, nil
}

// DeploymentPodPattern matches the pods a Deployment owns: <name>-<replicaset hash>-<pod suffix>.
// Prometheus anchors regex matchers, so "api" does not match pods of "api-gateway".
func DeploymentPodPattern(deployment string) string {
	return regexp.QuoteMeta(deployment) + `-[a-z0-9]{5,10}-[a-z0-9]{5}`
}

func cpuQuery(namespace, deployment, window string) string {
	return fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s", pod=~"%s", container!=""}[%s]))`, namespace, DeploymentPodPattern(deployment), window)
}

func memoryQuery(namespace, deployment string) string {
	return fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s", pod=~"%s", container!=""})`, namespace, DeploymentPodPattern(deployment))
}

// GetCPUUsage fetches the CPU usage for a specific deployment in a namespace, summed across its pods.
func (c *Client) GetCPUUsage(ctx context.Context, namespace, deployment, window string) (float64, error) {
	return c.executeQuery(ctx, cpuQuery(namespace, deployment, window))
}

// GetMemoryUsage fetches the Memory usage for a specific deployment, summed across its pods.
func (c *Client) GetMemoryUsage(ctx context.Context, namespace, deployment string) (float64, error) {
	// Memory is a gauge, so we don't need a rate window
	return c.executeQuery(ctx, memoryQuery(namespace, deployment))
}

// GetNetworkReceive fetches the Network Receive rate for a specific deployment.
func (c *Client) GetNetworkReceive(ctx context.Context, namespace, deployment, window string) (float64, error) {
	query := fmt.Sprintf(`sum(rate(container_network_receive_bytes_total{namespace="%s", pod=~"%s"}[%s]))`, namespace, DeploymentPodPattern(deployment), window)
	return c.executeQuery(ctx, query)
}

// GetNetworkTransmit fetches the Network Transmit rate for a specific deployment.
func (c *Client) GetNetworkTransmit(ctx context.Context, namespace, deployment, window string) (float64, error) {
	query := fmt.Sprintf(`sum(rate(container_network_transmit_bytes_total{namespace="%s", pod=~"%s"}[%s]))`, namespace, DeploymentPodPattern(deployment), window)
	return c.executeQuery(ctx, query)
}

// GetCPUUsageHistory returns the deployment's CPU usage over the configured history window, oldest first.
func (c *Client) GetCPUUsageHistory(ctx context.Context, namespace, deployment, window string) ([]float64, error) {
	return c.executeRangeQuery(ctx, cpuQuery(namespace, deployment, window))
}

// GetMemoryUsageHistory returns the deployment's memory usage over the configured history window, oldest first.
func (c *Client) GetMemoryUsageHistory(ctx context.Context, namespace, deployment string) ([]float64, error) {
	return c.executeRangeQuery(ctx, memoryQuery(namespace, deployment))
}

// executeRangeQuery runs PromQL over the configured history window and returns the first series.
func (c *Client) executeRangeQuery(ctx context.Context, query string) ([]float64, error) {
	timeout, err := time.ParseDuration(c.config.Prometheus.Timeout)
	if err != nil {
		timeout = 10 * time.Second
	}
	window, err := time.ParseDuration(c.config.Prometheus.HistoryWindow)
	if err != nil {
		return nil, fmt.Errorf("invalid prometheus.history_window: %w", err)
	}
	step, err := time.ParseDuration(c.config.Prometheus.HistoryStep)
	if err != nil || step <= 0 {
		return nil, fmt.Errorf("invalid prometheus.history_step: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	end := time.Now()
	result, warnings, err := c.v1api.QueryRange(ctx, query, v1.Range{Start: end.Add(-window), End: end, Step: step})
	if err != nil {
		return nil, fmt.Errorf("prometheus range query failed: %w", err)
	}
	if len(warnings) > 0 {
		slog.Warn("Prometheus range query returned warnings", slog.Any("warnings", warnings), slog.String("query", query))
	}

	matrix, ok := result.(model.Matrix)
	if !ok || len(matrix) == 0 {
		return []float64{}, nil
	}
	series := make([]float64, 0, len(matrix[0].Values))
	for _, sample := range matrix[0].Values {
		series = append(series, float64(sample.Value))
	}
	return series, nil
}
