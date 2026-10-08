package finops

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"watchdog-agent/internal/config"
)

// allocationFixture mirrors the shape returned by OpenCost 1.109 for aggregate=namespace,pod.
const allocationFixture = `{
	"code": 200,
	"data": [{
		"default/api-7d9f8c6b5d-abcde": {"totalCost": 1.0, "minutes": 1460, "properties": {"namespace": "default", "pod": "api-7d9f8c6b5d-abcde"}},
		"default/api-7d9f8c6b5d-fghij": {"totalCost": 1.0, "minutes": 1460, "properties": {"namespace": "default", "pod": "api-7d9f8c6b5d-fghij"}},
		"default/api-gateway-5c6d7e8f9a-klmno": {"totalCost": 2.0, "minutes": 730, "properties": {"namespace": "default", "pod": "api-gateway-5c6d7e8f9a-klmno"}},
		"shop/web-6b7c8d9e0f-pqrst": {"totalCost": 0.5, "minutes": 1460, "properties": {"namespace": "shop", "pod": "web-6b7c8d9e0f-pqrst"}},
		"__idle__": {"totalCost": 0.25, "minutes": 1460, "properties": {}},
		"shop/pending-pod": {"totalCost": 9.0, "minutes": 0, "properties": {"namespace": "shop", "pod": "pending-pod"}}
	}]
}`

func newTestClient(t *testing.T, body string, check func(*http.Request)) *Client {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return NewClient(&config.Config{OpenCost: config.OpenCostConfig{URL: ts.URL, Timeout: "5s", Window: "1d"}})
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestGetAllocations(t *testing.T) {
	client := newTestClient(t, allocationFixture, func(r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/allocation/compute" || q.Get("aggregate") != "namespace,pod" || q.Get("window") != "1d" || q.Get("accumulate") != "true" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
	})
	allocations, err := client.GetAllocations(context.Background())
	if err != nil {
		t.Fatalf("GetAllocations failed: %v", err)
	}

	// $1 over 1460 minutes is $30/month; $2 over 730 minutes is $120/month.
	apiPods := func(pod string) bool { return strings.HasPrefix(pod, "api-7d9f8c6b5d-") }
	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{"workload sums only its own pods", allocations.WorkloadCost("default", apiPods), 60},
		{"namespace sums all its pods", allocations.NamespaceCost("default"), 180},
		{"other namespace", allocations.NamespaceCost("shop"), 15},
		{"missing namespace", allocations.NamespaceCost("absent"), 0},
		{"cluster includes idle and skips zero-minute entries", allocations.ClusterCost(), 202.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !approx(tt.got, tt.want) {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestGetAllocations_Errors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"api error code", `{"code": 400, "message": "bad window", "data": null}`},
		{"malformed json", `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newTestClient(t, tt.body, nil).GetAllocations(context.Background()); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestGetAllocations_Empty(t *testing.T) {
	allocations, err := newTestClient(t, `{"code": 200, "data": [{}]}`, nil).GetAllocations(context.Background())
	if err != nil {
		t.Fatalf("GetAllocations failed: %v", err)
	}
	if allocations.ClusterCost() != 0 {
		t.Errorf("expected zero cost, got %v", allocations.ClusterCost())
	}
}
