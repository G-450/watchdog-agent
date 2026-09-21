package storage

import (
	"context"
	"database/sql"
	"path/filepath"
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
	snapshotID, err := store.SaveSnapshot(ctx, snap)
	if err != nil {
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

	latest, err := store.GetLatestSnapshot(ctx)
	if err != nil || latest == nil || latest.Nodes != 3 {
		t.Fatalf("Failed to retrieve latest snapshot: %+v, %v", latest, err)
	}

	recommendations := []*model.Recommendation{{
		Target: "default/my-app", Status: "Approved", ExpectedSavings: 1.25,
		ConfidenceScore: 0.9, Timestamp: now,
	}}
	if err := store.SaveRecommendations(ctx, snapshotID, recommendations); err != nil {
		t.Fatalf("Failed to save recommendations: %v", err)
	}
	storedRecommendations, err := store.GetRecommendations(ctx, RecommendationQuery{Status: "Approved", Limit: 10})
	if err != nil || len(storedRecommendations) != 1 {
		t.Fatalf("Failed to retrieve recommendations: %v", err)
	}
	if storedRecommendations[0].ID == 0 || storedRecommendations[0].ExpectedSavings != 1.25 {
		t.Fatalf("Unexpected stored recommendation: %+v", storedRecommendations[0])
	}
}

func saveCycle(t *testing.T, store Store, at time.Time, targets ...string) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := store.SaveSnapshot(ctx, &model.ClusterSnapshot{Timestamp: at, Nodes: 2, TotalCost: float64(at.Minute())})
	if err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}
	recs := make([]*model.Recommendation, 0, len(targets))
	for _, target := range targets {
		recs = append(recs, &model.Recommendation{Target: target, Status: "Approved", ExpectedSavings: 10, Timestamp: at})
	}
	if err := store.SaveRecommendations(ctx, id, recs); err != nil {
		t.Fatalf("SaveRecommendations failed: %v", err)
	}
	return id
}

func TestGetRecommendations_LatestOnly(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	base := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

	saveCycle(t, store, base, "default/api", "default/web")
	saveCycle(t, store, base.Add(time.Minute), "default/api")

	tests := []struct {
		name  string
		query RecommendationQuery
		want  int
	}{
		{"history keeps every cycle", RecommendationQuery{}, 3},
		{"latest keeps only the newest analysis", RecommendationQuery{LatestOnly: true}, 1},
		{"latest combines with status", RecommendationQuery{LatestOnly: true, Status: "Rejected"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.GetRecommendations(ctx, tt.query)
			if err != nil || len(got) != tt.want {
				t.Fatalf("got %d recommendations (err %v), want %d", len(got), err, tt.want)
			}
		})
	}

	// A later analysis with no findings clears the current set.
	saveCycle(t, store, base.Add(2*time.Minute))
	if got, _ := store.GetRecommendations(ctx, RecommendationQuery{LatestOnly: true}); len(got) != 0 {
		t.Errorf("expected no current recommendations after an empty analysis, got %d", len(got))
	}
}

func TestGetSnapshotSummaries_UsesUTCOrdering(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	ist := time.FixedZone("IST", 5*3600+1800)
	base := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

	// Saved with mixed zones: 10:00Z (as 15:30 IST), 10:05Z, 10:10Z (as 15:40 IST).
	for _, at := range []time.Time{base.In(ist), base.Add(5 * time.Minute), base.Add(10 * time.Minute).In(ist)} {
		if _, err := store.SaveSnapshot(ctx, &model.ClusterSnapshot{Timestamp: at, TotalCost: float64(at.UTC().Minute())}); err != nil {
			t.Fatalf("SaveSnapshot failed: %v", err)
		}
	}

	summaries, err := store.GetSnapshotSummaries(ctx, base.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("GetSnapshotSummaries failed: %v", err)
	}
	if len(summaries) != 2 || summaries[0].TotalCost != 5 || summaries[1].TotalCost != 10 {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}

	limited, _ := store.GetSnapshotSummaries(ctx, base.Add(-time.Hour), 1)
	if len(limited) != 1 || limited[0].TotalCost != 10 {
		t.Fatalf("limit should keep the newest snapshot, got %+v", limited)
	}

	latest, _ := store.GetLatestSnapshot(ctx)
	if latest == nil || latest.TotalCost != 10 {
		t.Fatalf("latest snapshot should be 10:10Z, got %+v", latest)
	}
}

func TestMigrateLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Schema and value format written by earlier agent versions.
	_, err = db.Exec(`
		CREATE TABLE cluster_snapshots (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME NOT NULL, nodes INTEGER NOT NULL, total_cost REAL NOT NULL, raw_data TEXT NOT NULL);
		CREATE TABLE recommendations (id INTEGER PRIMARY KEY AUTOINCREMENT, target TEXT NOT NULL, status TEXT NOT NULL, expected_savings REAL NOT NULL, confidence_score REAL NOT NULL, timestamp DATETIME NOT NULL, raw_data TEXT NOT NULL);
		INSERT INTO cluster_snapshots (timestamp, nodes, total_cost, raw_data) VALUES ('2026-09-21 14:34:06.3345076 +0530 IST m=+780.037669301', 2, 1.5, '{"nodes":2,"total_cost":1.5}');
		INSERT INTO recommendations (target, status, expected_savings, confidence_score, timestamp, raw_data) VALUES ('default/api', 'Approved', 1, 0.9, '2026-09-21 09:04:06 +0000 UTC', '{"target":"default/api"}');
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	defer store.Close()

	summaries, err := store.GetSnapshotSummaries(context.Background(), time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC), 10)
	if err != nil || len(summaries) != 1 {
		t.Fatalf("expected the migrated snapshot, got %+v (err %v)", summaries, err)
	}
	if want := time.Date(2026, 9, 21, 9, 4, 6, 334507600, time.UTC); !summaries[0].Timestamp.Equal(want) {
		t.Errorf("timestamp = %v, want %v", summaries[0].Timestamp, want)
	}
	if recs, err := store.GetRecommendations(context.Background(), RecommendationQuery{LatestOnly: true}); err != nil || len(recs) != 0 {
		t.Errorf("legacy recommendations have no analysis and should not be current: %d (err %v)", len(recs), err)
	}
}
