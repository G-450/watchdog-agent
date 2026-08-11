package finops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"watchdog-agent/internal/config"
)

func TestFinOpsClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"code": 200,
			"data": [
				{
					"cluster-id/namespace/pod": {
						"totalCost": 0.2,
						"cpuCost": 0.15,
						"ramCost": 0.05,
						"gpuCost": 0.0
					}
				}
			]
		}`))
	}))
	defer ts.Close()

	cfg := &config.Config{
		OpenCost: config.OpenCostConfig{
			URL:     ts.URL,
			Timeout: "5s",
		},
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Test GetNamespaceCost
	nsCost, err := client.GetNamespaceCost(ctx, "default", "1d")
	if err != nil {
		t.Fatalf("GetNamespaceCost failed: %v", err)
	}
	if nsCost.TotalCost != 0.2 {
		t.Errorf("Expected 0.2, got %v", nsCost.TotalCost)
	}

	// Test GetClusterCost
	clusterCost, err := client.GetClusterCost(ctx, "1d")
	if err != nil {
		t.Fatalf("GetClusterCost failed: %v", err)
	}
	if clusterCost.CPUCost != 0.15 {
		t.Errorf("Expected 0.15, got %v", clusterCost.CPUCost)
	}

	// Test GetDeploymentCost
	depCost, err := client.GetDeploymentCost(ctx, "default", "test-dep", "1d")
	if err != nil {
		t.Fatalf("GetDeploymentCost failed: %v", err)
	}
	if depCost.RAMCost != 0.05 {
		t.Errorf("Expected 0.05, got %v", depCost.RAMCost)
	}
}

func TestFinOpsClient_Empty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"code": 200,
			"data": []
		}`))
	}))
	defer ts.Close()

	cfg := &config.Config{
		OpenCost: config.OpenCostConfig{
			URL:     ts.URL,
			Timeout: "5s",
		},
	}

	client := NewClient(cfg)
	ctx := context.Background()

	nsCost, err := client.GetNamespaceCost(ctx, "default", "1d")
	if err != nil {
		t.Fatalf("GetNamespaceCost failed: %v", err)
	}
	if nsCost.TotalCost != 0.0 {
		t.Errorf("Expected 0.0, got %v", nsCost.TotalCost)
	}
}
