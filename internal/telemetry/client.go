package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"watchdog-agent/internal/config"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// Client handles communication with the Prometheus API to fetch resource usage metrics.
type Client struct {
	v1api v1.API
}

// NewClient initializes a new Telemetry client connecting to the given Prometheus URL.
func NewClient(cfg *config.Config) (*Client, error) {
	client, err := api.NewClient(api.Config{
		Address: cfg.Prometheus.URL,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating prometheus client: %v", err)
	}

	v1api := v1.NewAPI(client)
	return &Client{v1api: v1api}, nil
}

// GetCPUUsage fetches the CPU usage for a specific deployment in a namespace over the last 5 minutes.
func (c *Client) GetCPUUsage(ctx context.Context, namespace, deployment string) (string, error) {
	// Simple PromQL query to get rate of CPU usage for pods matching the deployment name
	query := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s", pod=~"%s-.*", container!=""}[5m]))`, namespace, deployment)

	result, warnings, err := c.v1api.Query(ctx, query, time.Now())
	if err != nil {
		return "", fmt.Errorf("error querying prometheus: %v", err)
	}
	if len(warnings) > 0 {
		slog.Warn("Prometheus query returned warnings", slog.Any("warnings", warnings))
	}

	// Format result
	vec, ok := result.(model.Vector)
	if !ok || len(vec) == 0 {
		return "0.0 (No data)", nil
	}

	return vec[0].Value.String(), nil
}
