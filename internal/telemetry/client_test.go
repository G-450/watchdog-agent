package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
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

func TestDeploymentPodPattern(t *testing.T) {
	pattern := regexp.MustCompile("^" + DeploymentPodPattern("api") + "$")
	tests := []struct {
		pod  string
		want bool
	}{
		{"api-59f8c7ddb5-94c5b", true},
		{"api-7d9f8c6b5-abcde", true},
		{"api-gateway-59f8c7ddb5-94c5b", false}, // a different deployment sharing the prefix
		{"api-0", false},                        // StatefulSet-style pod
		{"xapi-59f8c7ddb5-94c5b", false},
	}
	for _, tt := range tests {
		if got := pattern.MatchString(tt.pod); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.pod, got, tt.want)
		}
	}
	if got := DeploymentPodPattern("my.app"); got != `my\.app-[a-z0-9]{5,10}-[a-z0-9]{5}` {
		t.Errorf("deployment names must be escaped, got %s", got)
	}
}

func TestGetCPUUsageHistory(t *testing.T) {
	var query url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		query = r.Form
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1,"0.5"],[2,"0.75"],[3,"1"]]}]}}`))
	}))
	defer ts.Close()

	client, err := NewClient(&config.Config{Prometheus: config.PrometheusConfig{URL: ts.URL, Timeout: "5s", HistoryWindow: "6h", HistoryStep: "15m"}})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	series, err := client.GetCPUUsageHistory(context.Background(), "shop", "api", "5m")
	if err != nil {
		t.Fatalf("GetCPUUsageHistory failed: %v", err)
	}
	if len(series) != 3 || series[0] != 0.5 || series[2] != 1 {
		t.Errorf("unexpected series: %v", series)
	}
	if query.Get("step") != "900" || !strings.Contains(query.Get("query"), `pod=~"api-[a-z0-9]{5,10}-[a-z0-9]{5}"`) {
		t.Errorf("unexpected range query: %v", query)
	}
}

func TestGetCPUUsageHistory_InvalidConfig(t *testing.T) {
	client, _ := NewClient(&config.Config{Prometheus: config.PrometheusConfig{URL: "http://127.0.0.1:1", HistoryWindow: "soon", HistoryStep: "15m"}})
	if _, err := client.GetCPUUsageHistory(context.Background(), "shop", "api", "5m"); err == nil {
		t.Error("expected an error for an invalid history window")
	}
}
