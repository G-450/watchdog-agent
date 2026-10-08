package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"watchdog-agent/internal/model"
	"watchdog-agent/internal/storage"
)

func TestDashboardEndpoints(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("initialize store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	snapshot := &model.ClusterSnapshot{Timestamp: now, Nodes: 3, TotalCost: 12.5, Namespaces: map[string]*model.NamespaceSnapshot{
		"storefront": {Name: "storefront", Workloads: map[string]*model.WorkloadSnapshot{
			"checkout": {Name: "checkout", Namespace: "storefront", Type: "Deployment", Replicas: 2, CPUUsage: .3, CPURequests: .7, TotalCost: 4.5},
		}},
	}}
	snapshotID, err := store.SaveSnapshot(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	recommendations := []*model.Recommendation{{Target: "storefront/checkout", Status: "Approved", ExpectedSavings: 3.25, ConfidenceScore: .92, Timestamp: now}}
	if err := store.SaveRecommendations(context.Background(), snapshotID, recommendations); err != nil {
		t.Fatalf("save recommendations: %v", err)
	}

	handler := NewServer(store, "test-agent", []string{"http://localhost:3000"}).Handler()
	tests := []struct {
		path   string
		status int
	}{
		{"/health", http.StatusOK},
		{"/api/v1/status", http.StatusOK},
		{"/api/v1/overview", http.StatusOK},
		{"/api/v1/snapshots", http.StatusOK},
		{"/api/v1/workloads", http.StatusOK},
		{"/api/v1/workloads/storefront/checkout", http.StatusOK},
		{"/api/v1/workloads/storefront/missing", http.StatusNotFound},
		{"/api/v1/recommendations?status=Approved", http.StatusOK},
		{"/api/v1/recommendations?scope=all", http.StatusOK},
		{"/api/v1/recommendations?scope=bogus", http.StatusBadRequest},
		{"/api/v1/snapshots?since=yesterday", http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Origin", "http://localhost:3000")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("expected %d, got %d: %s", test.status, response.Code, response.Body.String())
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Errorf("expected JSON content type")
			}
			if response.Code < 400 && response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
				t.Errorf("expected allowed CORS origin")
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var overview struct {
		Workloads        int     `json:"workloads"`
		PotentialSavings float64 `json:"potential_savings"`
	}
	if err := json.NewDecoder(response.Body).Decode(&overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if overview.Workloads != 1 || overview.PotentialSavings != 3.25 {
		t.Fatalf("unexpected overview: %+v", overview)
	}
}

func getJSON(t *testing.T, handler http.Handler, path string, into interface{}) int {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if into != nil && response.Code == http.StatusOK {
		if err := json.NewDecoder(response.Body).Decode(into); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return response.Code
}

// TestOverviewCountsOnlyTheLatestAnalysis guards against summing the same saving once per cycle.
func TestOverviewCountsOnlyTheLatestAnalysis(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("initialize store: %v", err)
	}
	defer store.Close()
	handler := NewServer(store, "test-agent", nil).Handler()

	if code := getJSON(t, handler, "/api/v1/overview", nil); code != http.StatusNotFound {
		t.Fatalf("overview before any snapshot: expected 404, got %d", code)
	}

	base := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	for cycle := 0; cycle < 3; cycle++ {
		at := base.Add(time.Duration(cycle) * time.Minute)
		id, err := store.SaveSnapshot(context.Background(), &model.ClusterSnapshot{Timestamp: at, Nodes: 2, TotalCost: 100 + float64(cycle)})
		if err != nil {
			t.Fatalf("save snapshot: %v", err)
		}
		recs := []*model.Recommendation{
			{Target: "shop/api", Status: "Approved", ExpectedSavings: 10, Timestamp: at},
			{Target: "monitoring/grafana", Status: "Rejected", ExpectedSavings: 50, Timestamp: at},
		}
		if err := store.SaveRecommendations(context.Background(), id, recs); err != nil {
			t.Fatalf("save recommendations: %v", err)
		}
	}

	var overview struct {
		TotalCost        float64        `json:"total_cost"`
		PotentialSavings float64        `json:"potential_savings"`
		Recommendations  map[string]int `json:"recommendations"`
	}
	getJSON(t, handler, "/api/v1/overview", &overview)
	if overview.PotentialSavings != 10 || overview.Recommendations["approved"] != 1 || overview.Recommendations["rejected"] != 1 || overview.TotalCost != 102 {
		t.Fatalf("unexpected overview: %+v", overview)
	}

	tests := []struct {
		path string
		want int
	}{
		{"/api/v1/recommendations", 2},
		{"/api/v1/recommendations?scope=all", 6},
		{"/api/v1/recommendations?scope=all&status=Rejected", 3},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var body struct {
				Count int `json:"count"`
			}
			getJSON(t, handler, tt.path, &body)
			if body.Count != tt.want {
				t.Errorf("count = %d, want %d", body.Count, tt.want)
			}
		})
	}

	var snapshots struct {
		Items []map[string]interface{} `json:"items"`
	}
	getJSON(t, handler, "/api/v1/snapshots?since=2026-09-21T00:00:00Z&limit=2", &snapshots)
	if len(snapshots.Items) != 2 || snapshots.Items[1]["total_cost"] != 102.0 {
		t.Fatalf("expected the two newest snapshot summaries in time order, got %+v", snapshots.Items)
	}
	if _, heavy := snapshots.Items[0]["namespaces"]; heavy {
		t.Errorf("snapshot summaries should not include full namespace data")
	}
}
