package reasoning

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/model"
)

// contractFixture is shared with ai-service/tests so both sides agree on the request shape.
var contractFixture = filepath.Join("..", "..", "testdata", "analyze_request.json")

// contractSnapshot builds the snapshot and history encoded in the contract fixture.
func contractSnapshot() (*model.ClusterSnapshot, map[string]WorkloadHistory) {
	snapshot := &model.ClusterSnapshot{
		Timestamp: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
		ClusterID: "watchdog-agent-01",
		Nodes:     2,
		TotalCost: 120,
		Namespaces: map[string]*model.NamespaceSnapshot{
			"shop": {Name: "shop", NamespaceCost: 80, Workloads: map[string]*model.WorkloadSnapshot{
				// Over-provisioned: 3 replicas requesting 1 core each, using 0.6 cores in total.
				"api": {Name: "api", Namespace: "shop", Type: "Deployment", Replicas: 3,
					CPURequests: 1, CPULimits: 2, MemRequests: 1073741824, MemLimits: 2147483648,
					CPUUsage: 0.6, MemUsage: 943718400, NetRxUsage: 2048, NetTxUsage: 1024, TotalCost: 60,
					ServiceDependencies: []string{"api"}},
				// Hot: 2 replicas requesting 0.5 cores each, using 0.95 cores in total.
				"checkout": {Name: "checkout", Namespace: "shop", Type: "Deployment", Replicas: 2,
					CPURequests: 0.5, CPULimits: 1, MemRequests: 536870912, MemLimits: 1073741824,
					CPUUsage: 0.95, MemUsage: 629145600, TotalCost: 20},
			}},
			"monitoring": {Name: "monitoring", NamespaceCost: 40, Workloads: map[string]*model.WorkloadSnapshot{
				// Idle but protected by namespace policy.
				"grafana": {Name: "grafana", Namespace: "monitoring", Type: "Deployment", Replicas: 1,
					CPURequests: 1, MemRequests: 536870912, CPUUsage: 0.01, MemUsage: 104857600, TotalCost: 40},
			}},
		},
	}
	history := map[string]WorkloadHistory{
		HistoryKey("shop", "api"): {
			CPU:    []float64{0.55, 0.58, 0.6, 0.57, 0.62, 0.59, 0.6, 0.61},
			Memory: []float64{9.2e8, 9.3e8, 9.4e8, 9.3e8, 9.4e8, 9.4e8, 9.4e8, 9.4e8},
		},
		HistoryKey("shop", "checkout"): {
			CPU:    []float64{0.9, 0.92, 0.95, 0.94, 0.96, 0.95},
			Memory: []float64{6.2e8, 6.3e8, 6.3e8, 6.3e8, 6.3e8, 6.3e8},
		},
	}
	return snapshot, history
}

func TestAnalyzeRequestMatchesContract(t *testing.T) {
	snapshot, history := contractSnapshot()
	got, err := json.MarshalIndent(newAnalyzeRequest(snapshot, history), "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	got = append(got, '\n')

	if os.Getenv("UPDATE_CONTRACT") == "1" {
		if err := os.WriteFile(contractFixture, got, 0o644); err != nil {
			t.Fatalf("failed to update fixture: %v", err)
		}
	}
	want, err := os.ReadFile(contractFixture)
	if err != nil {
		t.Fatalf("failed to read fixture (run with UPDATE_CONTRACT=1 to create it): %v", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Errorf("analyze request no longer matches %s; if the change is intended, run with UPDATE_CONTRACT=1 and make sure ai-service still accepts it.\ngot:\n%s", contractFixture, got)
	}
}

func TestClient_Analyze(t *testing.T) {
	mockRecs := []*model.Recommendation{{Target: "shop/api", Action: "RIGHTSIZE_CPU_DOWN", Status: "Pending"}}

	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/analyze" {
			t.Errorf("Expected path /api/v1/analyze, got %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockRecs)
	}))
	defer server.Close()

	client := NewClient(&config.Config{AIService: config.AIServiceConfig{URL: server.URL}})
	snapshot, history := contractSnapshot()
	recs, err := client.Analyze(context.Background(), snapshot, history)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}
	if len(recs) != 1 || recs[0].Target != "shop/api" || recs[0].Action != "RIGHTSIZE_CPU_DOWN" {
		t.Fatalf("unexpected recommendations: %+v", recs)
	}

	workload := received["namespaces"].(map[string]interface{})["shop"].(map[string]interface{})["workloads"].(map[string]interface{})["api"].(map[string]interface{})
	if workload["cpu_requests"] != 1.0 || len(workload["cpu_history"].([]interface{})) != 8 {
		t.Errorf("request is missing snake_case fields or history: %v", workload)
	}
	// Workloads without history still send empty lists rather than null.
	grafana := received["namespaces"].(map[string]interface{})["monitoring"].(map[string]interface{})["workloads"].(map[string]interface{})["grafana"].(map[string]interface{})
	if history, ok := grafana["cpu_history"].([]interface{}); !ok || len(history) != 0 {
		t.Errorf("expected empty cpu_history, got %v", grafana["cpu_history"])
	}
}

func TestClient_AnalyzeErrorIncludesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"Reasoning error: boom"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(&config.Config{AIService: config.AIServiceConfig{URL: server.URL}})
	_, err := client.Analyze(context.Background(), &model.ClusterSnapshot{}, nil)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("boom")) {
		t.Fatalf("expected error to include the response body, got %v", err)
	}
}
