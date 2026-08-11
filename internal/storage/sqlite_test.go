package storage

import (
	"context"
	"testing"
	"time"

	"watchdog-agent/internal/model"
)

func TestSQLiteStore(t *testing.T) {
	// Initialize in-memory database for testing
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Create a dummy snapshot
	snap := &model.ClusterSnapshot{
		Timestamp: now,
		Nodes:     3,
		TotalCost: 15.5,
		Namespaces: map[string]*model.NamespaceSnapshot{
			"default": {
				Name:          "default",
				NamespaceCost: 5.5,
				Workloads: map[string]*model.WorkloadSnapshot{
					"my-app": {
						Name:        "my-app",
						Namespace:   "default",
						Type:        "Deployment",
						Replicas:    2,
						CPURequests: 0.5,
						CPULimits:   1.0,
						MemRequests: 256000000,
						MemLimits:   512000000,
						TotalCost:   2.5,
						IsExcluded:  false,
					},
				},
			},
		},
	}

	// Test SaveSnapshot
	if err := store.SaveSnapshot(ctx, snap); err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Test GetSnapshots
	snaps, err := store.GetSnapshots(ctx, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("Failed to get snapshots: %v", err)
	}

	if len(snaps) != 1 {
		t.Fatalf("Expected 1 snapshot, got %d", len(snaps))
	}

	retrieved := snaps[0]
	if retrieved.Nodes != 3 {
		t.Errorf("Expected 3 nodes, got %d", retrieved.Nodes)
	}
	if retrieved.TotalCost != 15.5 {
		t.Errorf("Expected total cost 15.5, got %f", retrieved.TotalCost)
	}
	
	if len(retrieved.Namespaces) != 1 {
		t.Fatalf("Expected 1 namespace, got %d", len(retrieved.Namespaces))
	}

	ns, ok := retrieved.Namespaces["default"]
	if !ok {
		t.Fatalf("Expected 'default' namespace")
	}

	if ns.NamespaceCost != 5.5 {
		t.Errorf("Expected namespace cost 5.5, got %f", ns.NamespaceCost)
	}

	wl, ok := ns.Workloads["my-app"]
	if !ok {
		t.Fatalf("Expected 'my-app' workload")
	}

	if wl.Replicas != 2 {
		t.Errorf("Expected 2 replicas, got %d", wl.Replicas)
	}
}
