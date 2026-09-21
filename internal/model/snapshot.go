package model

import (
	"time"
)

// ClusterSnapshot represents a single reconciliation cycle's snapshot.
type ClusterSnapshot struct {
	Timestamp  time.Time                     `json:"timestamp"`
	ClusterID  string                        `json:"cluster_id"`
	Nodes      int                           `json:"nodes"`
	Namespaces map[string]*NamespaceSnapshot `json:"namespaces"`
	TotalCost  float64                       `json:"total_cost"`
}

// NamespaceSnapshot represents a namespace's state.
type NamespaceSnapshot struct {
	Name          string                       `json:"name"`
	Workloads     map[string]*WorkloadSnapshot `json:"workloads"`
	NamespaceCost float64                      `json:"namespace_cost"`
}

// WorkloadSnapshot represents a unified snapshot of a workload.
type WorkloadSnapshot struct {
	// Metadata
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"` // e.g., Deployment, StatefulSet
	Replicas  int32  `json:"replicas"`

	// Resources (requests and limits from K8s API), per replica
	CPURequests float64 `json:"cpu_requests"` // in cores
	CPULimits   float64 `json:"cpu_limits"`
	MemRequests int64   `json:"mem_requests"` // in bytes
	MemLimits   int64   `json:"mem_limits"`

	// Utilization (from Telemetry), summed across all replicas
	CPUUsage   float64 `json:"cpu_usage"`    // in cores
	MemUsage   float64 `json:"mem_usage"`    // in bytes
	NetRxUsage float64 `json:"net_rx_usage"` // in bytes per second
	NetTxUsage float64 `json:"net_tx_usage"` // in bytes per second

	// Cost (from FinOps), projected to a monthly run-rate in USD
	TotalCost float64 `json:"total_cost"`

	// Exclusion Status
	IsExcluded    bool   `json:"is_excluded"`
	ExcludeReason string `json:"exclude_reason"`

	// Dependencies
	ServiceDependencies []string `json:"service_dependencies"`
}

// SnapshotSummary is the lightweight view of a cluster snapshot used for cost trends.
type SnapshotSummary struct {
	Timestamp time.Time `json:"timestamp"`
	Nodes     int       `json:"nodes"`
	TotalCost float64   `json:"total_cost"`
}
