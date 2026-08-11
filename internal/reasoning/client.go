package reasoning

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// Analyze sends the cluster snapshot to the AI service and receives recommendations.
func (c *Client) Analyze(ctx context.Context, snapshot *model.ClusterSnapshot) ([]*model.Recommendation, error) {
	data, err := json.Marshal(snapshot)
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
		return nil, fmt.Errorf("ai service returned non-200 status: %d", resp.StatusCode)
	}

	var recs []*model.Recommendation
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return recs, nil
}
