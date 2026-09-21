package reasoning

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"watchdog-agent/internal/config"
	"watchdog-agent/internal/model"
)

// Client for the Python AI Service
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(cfg *config.Config) *Client {
	url := cfg.AIService.URL
	if url == "" {
		url = "http://localhost:8000"
	}
	return &Client{
		baseURL: url,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WorkloadHistory is a workload's recent usage, oldest first, in the units of WorkloadSnapshot.
type WorkloadHistory struct {
	CPU    []float64
	Memory []float64
}

// HistoryKey identifies a workload in the history map passed to Analyze.
func HistoryKey(namespace, name string) string {
	return namespace + "/" + name
}

// workloadPayload is a workload snapshot plus the history the forecaster needs. History is sent
// only to the AI service; it is not part of the persisted snapshot.
type workloadPayload struct {
	*model.WorkloadSnapshot
	CPUHistory []float64 `json:"cpu_history"`
	MemHistory []float64 `json:"mem_history"`
}

type namespacePayload struct {
	Name          string                      `json:"name"`
	Workloads     map[string]*workloadPayload `json:"workloads"`
	NamespaceCost float64                     `json:"namespace_cost"`
}

type analyzeRequest struct {
	Timestamp  time.Time                    `json:"timestamp"`
	ClusterID  string                       `json:"cluster_id"`
	Nodes      int                          `json:"nodes"`
	Namespaces map[string]*namespacePayload `json:"namespaces"`
	TotalCost  float64                      `json:"total_cost"`
}

func newAnalyzeRequest(snapshot *model.ClusterSnapshot, history map[string]WorkloadHistory) analyzeRequest {
	req := analyzeRequest{
		Timestamp: snapshot.Timestamp, ClusterID: snapshot.ClusterID, Nodes: snapshot.Nodes,
		TotalCost: snapshot.TotalCost, Namespaces: make(map[string]*namespacePayload, len(snapshot.Namespaces)),
	}
	for nsName, ns := range snapshot.Namespaces {
		nsPayload := &namespacePayload{Name: ns.Name, NamespaceCost: ns.NamespaceCost, Workloads: make(map[string]*workloadPayload, len(ns.Workloads))}
		for wlName, wl := range ns.Workloads {
			h := history[HistoryKey(wl.Namespace, wl.Name)]
			nsPayload.Workloads[wlName] = &workloadPayload{WorkloadSnapshot: wl, CPUHistory: nonNil(h.CPU), MemHistory: nonNil(h.Memory)}
		}
		req.Namespaces[nsName] = nsPayload
	}
	return req
}

func nonNil(values []float64) []float64 {
	if values == nil {
		return []float64{}
	}
	return values
}

// Analyze sends the cluster snapshot and workload usage history to the AI service and receives recommendations.
func (c *Client) Analyze(ctx context.Context, snapshot *model.ClusterSnapshot, history map[string]WorkloadHistory) ([]*model.Recommendation, error) {
	data, err := json.Marshal(newAnalyzeRequest(snapshot, history))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/analyze", bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("ai service returned status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var recs []*model.Recommendation
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return recs, nil
}
