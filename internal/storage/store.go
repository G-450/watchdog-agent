package storage

import (
	"context"
	"time"

	"watchdog-agent/internal/model"
)

// RecommendationQuery selects recommendations to read back.
type RecommendationQuery struct {
	Status string // exact status, e.g. "Approved"; empty for any
	Limit  int
	// LatestOnly restricts results to the most recent completed analysis, so repeated
	// cycles recommending the same change are not counted more than once.
	LatestOnly bool
}

// Store defines the interface for persisting Watchdog snapshots.
type Store interface {
	// SaveSnapshot persists a complete cluster snapshot and returns its ID.
	SaveSnapshot(ctx context.Context, snapshot *model.ClusterSnapshot) (int64, error)

	// GetSnapshots retrieves snapshots that occurred after the specified time.
	GetSnapshots(ctx context.Context, since time.Time) ([]*model.ClusterSnapshot, error)

	// GetSnapshotSummaries retrieves up to limit of the newest snapshot summaries since the given time, oldest first.
	GetSnapshotSummaries(ctx context.Context, since time.Time, limit int) ([]model.SnapshotSummary, error)

	// GetLatestSnapshot retrieves the most recently collected cluster snapshot.
	GetLatestSnapshot(ctx context.Context) (*model.ClusterSnapshot, error)

	// SaveRecommendations persists the validated result of analysing one snapshot and marks
	// that snapshot as analysed, even when there are no recommendations.
	SaveRecommendations(ctx context.Context, snapshotID int64, recommendations []*model.Recommendation) error

	// GetRecommendations retrieves the newest recommendations matching the query.
	GetRecommendations(ctx context.Context, query RecommendationQuery) ([]*model.Recommendation, error)

	// Close cleanly closes the storage connection.
	Close() error
}
