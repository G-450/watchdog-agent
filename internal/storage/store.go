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

	// SaveVerification creates or replaces the verification for v.PR.
	SaveVerification(ctx context.Context, v *model.Verification) error

	// GetVerification returns the verification for a PR, or nil if there is none.
	GetVerification(ctx context.Context, pr int) (*model.Verification, error)

	// GetVerifications returns up to limit verifications, most recently merged first.
	GetVerifications(ctx context.Context, limit int) ([]*model.Verification, error)

	// SetHaltedWorkloads replaces the set of workloads Watchdog has stopped changing.
	SetHaltedWorkloads(ctx context.Context, halted []model.HaltedWorkload) error

	// GetHaltedWorkloads returns the workloads Watchdog has stopped changing.
	GetHaltedWorkloads(ctx context.Context) ([]model.HaltedWorkload, error)

	// Close cleanly closes the storage connection.
	Close() error
}
