package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"watchdog-agent/internal/model"
)

type sqliteStore struct {
	db *sql.DB
}

// NewSQLiteStore initializes a new SQLite-backed store.
func NewSQLiteStore(dbPath string) (Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Create tables if they do not exist.
	// Since the requirement says "SQLite or file-based JSON time-series store",
	// a simple schema that serializes the structured data to JSON is robust and easy to query/extend.
	// We'll have a main cluster_snapshots table and a workload_snapshots table for deeper queries.
	schema := `
	CREATE TABLE IF NOT EXISTS cluster_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		nodes INTEGER NOT NULL,
		total_cost REAL NOT NULL,
		raw_data TEXT NOT NULL
	);
	
	CREATE TABLE IF NOT EXISTS workload_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		cluster_snapshot_id INTEGER,
		timestamp DATETIME NOT NULL,
		namespace TEXT NOT NULL,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		replicas INTEGER,
		cpu_usage REAL,
		mem_usage REAL,
		total_cost REAL,
		is_excluded BOOLEAN,
		raw_data TEXT NOT NULL,
		FOREIGN KEY(cluster_snapshot_id) REFERENCES cluster_snapshots(id)
	);
	`
	
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) SaveSnapshot(ctx context.Context, snap *model.ClusterSnapshot) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	rawData, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster snapshot: %w", err)
	}

	// Insert cluster snapshot
	res, err := tx.ExecContext(ctx,
		`INSERT INTO cluster_snapshots (timestamp, nodes, total_cost, raw_data) VALUES (?, ?, ?, ?)`,
		snap.Timestamp, snap.Nodes, snap.TotalCost, string(rawData),
	)
	if err != nil {
		return fmt.Errorf("failed to insert cluster snapshot: %w", err)
	}

	clusterID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}

	// Insert individual workloads for easier querying later
	for _, ns := range snap.Namespaces {
		for _, wl := range ns.Workloads {
			wlRaw, err := json.Marshal(wl)
			if err != nil {
				return fmt.Errorf("failed to marshal workload snapshot: %w", err)
			}

			_, err = tx.ExecContext(ctx,
				`INSERT INTO workload_snapshots 
				(cluster_snapshot_id, timestamp, namespace, name, type, replicas, cpu_usage, mem_usage, total_cost, is_excluded, raw_data) 
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				clusterID, snap.Timestamp, wl.Namespace, wl.Name, wl.Type, wl.Replicas, wl.CPUUsage, wl.MemUsage, wl.TotalCost, wl.IsExcluded, string(wlRaw),
			)
			if err != nil {
				return fmt.Errorf("failed to insert workload snapshot: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (s *sqliteStore) GetSnapshots(ctx context.Context, since time.Time) ([]*model.ClusterSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT raw_data FROM cluster_snapshots WHERE timestamp >= ? ORDER BY timestamp ASC`, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []*model.ClusterSnapshot
	for rows.Next() {
		var rawData string
		if err := rows.Scan(&rawData); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		var snap model.ClusterSnapshot
		if err := json.Unmarshal([]byte(rawData), &snap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
		}

		snapshots = append(snapshots, &snap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return snapshots, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
