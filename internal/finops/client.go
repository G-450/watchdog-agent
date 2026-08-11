package finops

import (
	"watchdog-agent/internal/config"
)

// Client handles communication with the OpenCost API for real-time pricing data.
type Client struct {
	// TODO: Add the OpenCost HTTP connection client.
}

// NewClient initializes a new FinOps client.
func NewClient(cfg *config.Config) *Client {
	return &Client{}
}
