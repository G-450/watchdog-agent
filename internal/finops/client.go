package finops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Client handles communication with the OpenCost API to fetch financial metrics.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// OpenCostResponse maps the expected JSON response from the OpenCost /allocation/compute API.
type OpenCostResponse struct {
	Code int                     `json:"code"`
	Data []map[string]Allocation `json:"data"`
}

type Allocation struct {
	Name      string  `json:"name"`
	TotalCost float64 `json:"totalCost"`
	CPUCost   float64 `json:"cpuCost"`
	RAMCost   float64 `json:"ramCost"`
}

// NewClient initializes a new FinOps client connecting to the given OpenCost URL.
func NewClient(openCostURL string) *Client {
	return &Client{
		BaseURL: openCostURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetDeploymentCost fetches the total cost of a deployment in a namespace over the specified window (e.g., "1d", "1h").
func (c *Client) GetDeploymentCost(ctx context.Context, namespace, deployment, window string) (*Allocation, error) {
	// Build the API URL. We aggregate by namespace and deployment to get granular costs.
	reqURL, err := url.Parse(fmt.Sprintf("%s/allocation/compute", c.BaseURL))
	if err != nil {
		return nil, err
	}

	q := reqURL.Query()
	q.Set("window", window)
	q.Set("aggregate", "namespace,deployment")
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var costResp OpenCostResponse
	if err := json.NewDecoder(resp.Body).Decode(&costResp); err != nil {
		return nil, fmt.Errorf("error decoding json: %v", err)
	}

	if costResp.Code != 200 || len(costResp.Data) == 0 {
		return nil, fmt.Errorf("no data returned from OpenCost API")
	}

	// OpenCost returns a list of windows in "Data". Since we queried one window, we look at Data[0].
	// The map keys are the aggregated fields. Since we aggregated by "namespace,deployment",
	// the key format will be "namespace/deployment".
	key := fmt.Sprintf("%s/%s", namespace, deployment)

	allocations := costResp.Data[0]
	if alloc, exists := allocations[key]; exists {
		// Sometimes OpenCost returns __unallocated__ or 0 values, we return what we find.
		return &alloc, nil
	}

	return nil, fmt.Errorf("deployment %s not found in namespace %s in the given window", deployment, namespace)
}
