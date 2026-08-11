package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"watchdog-agent/internal/config"
)

func TestTelemetryClient(t *testing.T) {
	// Mock Prometheus API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return a mock vector response
		w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{
						"metric": {},
						"value": [1435781451.781, "1.5"]
					}
				]
			}
		}`))
	}))
	defer ts.Close()

	cfg := &config.Config{
		Prometheus: config.PrometheusConfig{
			URL:     ts.URL,
			Timeout: "5s",
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	// Test GetCPUUsage
	cpu, err := client.GetCPUUsage(ctx, "default", "test-dep", "5m")
	if err != nil {
		t.Errorf("GetCPUUsage failed: %v", err)
	}
	if cpu != 1.5 {
		t.Errorf("Expected 1.5, got %v", cpu)
	}

	// Test GetMemoryUsage
	mem, err := client.GetMemoryUsage(ctx, "default", "test-dep")
	if err != nil {
		t.Errorf("GetMemoryUsage failed: %v", err)
	}
	if mem != 1.5 {
		t.Errorf("Expected 1.5, got %v", mem)
	}

	// Test GetNetworkReceive
	netRx, err := client.GetNetworkReceive(ctx, "default", "test-dep", "5m")
	if err != nil {
		t.Errorf("GetNetworkReceive failed: %v", err)
	}
	if netRx != 1.5 {
		t.Errorf("Expected 1.5, got %v", netRx)
	}

	// Test GetNetworkTransmit
	netTx, err := client.GetNetworkTransmit(ctx, "default", "test-dep", "5m")
	if err != nil {
		t.Errorf("GetNetworkTransmit failed: %v", err)
	}
	if netTx != 1.5 {
		t.Errorf("Expected 1.5, got %v", netTx)
	}
}

func TestTelemetryClient_NoData(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": []
			}
		}`))
	}))
	defer ts.Close()

	cfg := &config.Config{
		Prometheus: config.PrometheusConfig{
			URL:     ts.URL,
			Timeout: "5s",
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	cpu, err := client.GetCPUUsage(context.Background(), "default", "test-dep", "5m")
	if err != nil {
		t.Errorf("GetCPUUsage failed: %v", err)
	}
	if cpu != 0.0 {
		t.Errorf("Expected 0.0, got %v", cpu)
	}
}
