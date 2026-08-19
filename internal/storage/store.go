package storage

import (
	"context"
	"time"

	"watchdog-agent/internal/model"
)

// Store defines the interface for persisting Watchdog snapshots.
type Store interface {
	// SaveSnapshot persists a complete cluster snapshot to the storage backend.
	SaveSnapshot(ctx context.Context, snapshot *model.ClusterSnapshot) error

	// GetSnapshots retrieves snapshots that occurred after the specified time.
	GetSnapshots(ctx context.Context, since time.Time) ([]*model.ClusterSnapshot, error)

	// GetLatestSnapshot retrieves the most recently collected cluster snapshot.
	GetLatestSnapshot(ctx context.Context) (*model.ClusterSnapshot, error)

	// SaveRecommendations persists validated recommendations.
	SaveRecommendations(ctx context.Context, recommendations []*model.Recommendation) error

	// GetRecommendations retrieves the newest recommendations, optionally filtered by status.
	GetRecommendations(ctx context.Context, status string, limit int) ([]*model.Recommendation, error)

	// Close cleanly closes the storage connection.
	Close() error
}
