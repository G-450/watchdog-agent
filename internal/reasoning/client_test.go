package reasoning

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/model"
)

func TestClient_Analyze(t *testing.T) {
	mockRecs := []*model.Recommendation{
		{
			Target: "default/mock",
			Status: "Pending",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/analyze" {
			t.Errorf("Expected path /api/v1/analyze, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockRecs)
	}))
	defer server.Close()

	cfg := &config.Config{
		AIService: config.AIServiceConfig{
			URL: server.URL,
		},
	}

	client := NewClient(cfg)

	snapshot := &model.ClusterSnapshot{}
	recs, err := client.Analyze(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(recs) != 1 {
		t.Fatalf("Expected 1 recommendation, got %d", len(recs))
	}

	if recs[0].Target != "default/mock" {
		t.Errorf("Expected target default/mock, got %s", recs[0].Target)
	}
}
