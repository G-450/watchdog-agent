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
	if err := store.SaveSnapshot(context.Background(), snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	recommendations := []*model.Recommendation{{Target: "storefront/checkout", Status: "Approved", ExpectedSavings: 3.25, ConfidenceScore: .92, Timestamp: now}}
	if err := store.SaveRecommendations(context.Background(), recommendations); err != nil {
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
			if response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
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
