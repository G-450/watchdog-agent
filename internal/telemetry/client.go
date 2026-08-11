package telemetry

import (
	"context"
	"fmt"
	"log/slog"
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

// GetCPUUsage fetches the CPU usage for a specific deployment in a namespace.
func (c *Client) GetCPUUsage(ctx context.Context, namespace, deployment, window string) (float64, error) {
	query := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s", pod=~"%s-.*", container!=""}[%s]))`, namespace, deployment, window)
	return c.executeQuery(ctx, query)
}

// GetMemoryUsage fetches the Memory usage for a specific deployment.
func (c *Client) GetMemoryUsage(ctx context.Context, namespace, deployment string) (float64, error) {
	// Memory is a gauge, so we don't need a rate window
	query := fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s", pod=~"%s-.*", container!=""})`, namespace, deployment)
	return c.executeQuery(ctx, query)
}

// GetNetworkReceive fetches the Network Receive rate for a specific deployment.
func (c *Client) GetNetworkReceive(ctx context.Context, namespace, deployment, window string) (float64, error) {
	query := fmt.Sprintf(`sum(rate(container_network_receive_bytes_total{namespace="%s", pod=~"%s-.*"}[%s]))`, namespace, deployment, window)
	return c.executeQuery(ctx, query)
}

// GetNetworkTransmit fetches the Network Transmit rate for a specific deployment.
func (c *Client) GetNetworkTransmit(ctx context.Context, namespace, deployment, window string) (float64, error) {
	query := fmt.Sprintf(`sum(rate(container_network_transmit_bytes_total{namespace="%s", pod=~"%s-.*"}[%s]))`, namespace, deployment, window)
	return c.executeQuery(ctx, query)
}
