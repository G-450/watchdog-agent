package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"watchdog-agent/internal/model"
)

// timestampLayout is fixed-width UTC so that SQL string comparison and ordering match time order.
const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// legacyTimestampLayouts are formats earlier versions wrote, starting with Go's time.String form.
var legacyTimestampLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999-07:00",
	time.RFC3339Nano,
}

func formatTimestamp(t time.Time) string {
	return t.UTC().Format(timestampLayout)
}

// parseTimestamp reads a stored timestamp in the current or any legacy format.
func parseTimestamp(value string) (time.Time, error) {
	value, _, _ = strings.Cut(value, " m=") // drop Go's monotonic clock suffix
	for _, layout := range legacyTimestampLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp %q", value)
}

type sqliteStore struct {
	db *sql.DB
}

// NewSQLiteStore initializes a new SQLite-backed store.
func NewSQLiteStore(dbPath string) (Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	// An in-memory database exists per connection, and SQLite serialises writers anyway.
	db.SetMaxOpenConns(1)

	// Structured data is serialised to JSON in raw_data; the other columns support querying.
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

	CREATE TABLE IF NOT EXISTS recommendations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		target TEXT NOT NULL,
		status TEXT NOT NULL,
		expected_savings REAL NOT NULL,
		confidence_score REAL NOT NULL,
		timestamp DATETIME NOT NULL,
		raw_data TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_recommendations_timestamp ON recommendations(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_recommendations_status ON recommendations(status);
	CREATE INDEX IF NOT EXISTS idx_cluster_snapshots_timestamp ON cluster_snapshots(timestamp);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

// migrate upgrades databases created by earlier versions of the agent.
func migrate(db *sql.DB) error {
	columns := []struct{ table, column, definition string }{
		{"cluster_snapshots", "analyzed", "BOOLEAN NOT NULL DEFAULT 0"},
		{"recommendations", "snapshot_id", "INTEGER REFERENCES cluster_snapshots(id)"},
	}
	for _, c := range columns {
		exists, err := columnExists(db, c.table, c.column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", c.table, c.column, c.definition)); err != nil {
				return fmt.Errorf("failed to add %s.%s: %w", c.table, c.column, err)
			}
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_recommendations_snapshot ON recommendations(snapshot_id)`); err != nil {
		return fmt.Errorf("failed to index recommendations: %w", err)
	}
	for _, table := range []string{"cluster_snapshots", "workload_snapshots", "recommendations"} {
		if err := normalizeTimestamps(db, table); err != nil {
			return err
		}
	}
	return nil
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, fmt.Errorf("failed to inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, kind       string
			defaultValue     sql.NullString
		)
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("failed to scan %s columns: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// normalizeTimestamps rewrites legacy "2006-01-02 15:04:05 -0700 MST m=+1.0" values as fixed-width UTC.
func normalizeTimestamps(db *sql.DB, table string) error {
	// CAST keeps the driver from converting DATETIME text before we see it.
	rows, err := db.Query(fmt.Sprintf("SELECT id, CAST(timestamp AS TEXT) FROM %s WHERE timestamp NOT LIKE '____-__-__T%%Z'", table))
	if err != nil {
		return fmt.Errorf("failed to read legacy timestamps from %s: %w", table, err)
	}
	updates := map[int64]string{}
	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan legacy timestamp: %w", err)
		}
		parsed, err := parseTimestamp(value)
		if err != nil {
			continue // leave values we cannot interpret untouched
		}
		updates[id] = formatTimestamp(parsed)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, value := range updates {
		if _, err := tx.Exec(fmt.Sprintf("UPDATE %s SET timestamp = ? WHERE id = ?", table), value, id); err != nil {
			return fmt.Errorf("failed to normalise timestamp in %s: %w", table, err)
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) SaveSnapshot(ctx context.Context, snap *model.ClusterSnapshot) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	rawData, err := json.Marshal(snap)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal cluster snapshot: %w", err)
	}

	timestamp := formatTimestamp(snap.Timestamp)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO cluster_snapshots (timestamp, nodes, total_cost, raw_data) VALUES (?, ?, ?, ?)`,
		timestamp, snap.Nodes, snap.TotalCost, string(rawData),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert cluster snapshot: %w", err)
	}

	snapshotID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	// Insert individual workloads for easier querying later
	for _, ns := range snap.Namespaces {
		for _, wl := range ns.Workloads {
			wlRaw, err := json.Marshal(wl)
			if err != nil {
				return 0, fmt.Errorf("failed to marshal workload snapshot: %w", err)
			}

			_, err = tx.ExecContext(ctx,
				`INSERT INTO workload_snapshots
				(cluster_snapshot_id, timestamp, namespace, name, type, replicas, cpu_usage, mem_usage, total_cost, is_excluded, raw_data)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				snapshotID, timestamp, wl.Namespace, wl.Name, wl.Type, wl.Replicas, wl.CPUUsage, wl.MemUsage, wl.TotalCost, wl.IsExcluded, string(wlRaw),
			)
			if err != nil {
				return 0, fmt.Errorf("failed to insert workload snapshot: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit snapshot: %w", err)
	}
	return snapshotID, nil
}

func (s *sqliteStore) GetSnapshots(ctx context.Context, since time.Time) ([]*model.ClusterSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT raw_data FROM cluster_snapshots WHERE timestamp >= ? ORDER BY timestamp ASC`, formatTimestamp(since))
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

func (s *sqliteStore) GetSnapshotSummaries(ctx context.Context, since time.Time, limit int) ([]model.SnapshotSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT CAST(timestamp AS TEXT), nodes, total_cost FROM cluster_snapshots WHERE timestamp >= ? ORDER BY timestamp DESC LIMIT ?`,
		formatTimestamp(since), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query snapshot summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]model.SnapshotSummary, 0)
	for rows.Next() {
		var timestamp string
		var summary model.SnapshotSummary
		if err := rows.Scan(&timestamp, &summary.Nodes, &summary.TotalCost); err != nil {
			return nil, fmt.Errorf("failed to scan snapshot summary: %w", err)
		}
		if summary.Timestamp, err = parseTimestamp(timestamp); err != nil {
			return nil, fmt.Errorf("failed to parse snapshot timestamp: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("snapshot summary rows error: %w", err)
	}
	// Newest rows were selected so the limit keeps recent data; return them in time order.
	for i, j := 0, len(summaries)-1; i < j; i, j = i+1, j-1 {
		summaries[i], summaries[j] = summaries[j], summaries[i]
	}
	return summaries, nil
}

func (s *sqliteStore) GetLatestSnapshot(ctx context.Context) (*model.ClusterSnapshot, error) {
	var rawData string
	err := s.db.QueryRowContext(ctx, `SELECT raw_data FROM cluster_snapshots ORDER BY timestamp DESC LIMIT 1`).Scan(&rawData)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query latest snapshot: %w", err)
	}

	var snapshot model.ClusterSnapshot
	if err := json.Unmarshal([]byte(rawData), &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal latest snapshot: %w", err)
	}
	return &snapshot, nil
}

func (s *sqliteStore) SaveRecommendations(ctx context.Context, snapshotID int64, recommendations []*model.Recommendation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin recommendation transaction: %w", err)
	}
	defer tx.Rollback()

	for _, recommendation := range recommendations {
		rawData, err := json.Marshal(recommendation)
		if err != nil {
			return fmt.Errorf("failed to marshal recommendation: %w", err)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO recommendations
			(snapshot_id, target, status, expected_savings, confidence_score, timestamp, raw_data)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, snapshotID, recommendation.Target, recommendation.Status,
			recommendation.ExpectedSavings, recommendation.ConfidenceScore,
			formatTimestamp(recommendation.Timestamp), string(rawData))
		if err != nil {
			return fmt.Errorf("failed to insert recommendation: %w", err)
		}
		recommendation.ID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to read recommendation id: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cluster_snapshots SET analyzed = 1 WHERE id = ?`, snapshotID); err != nil {
		return fmt.Errorf("failed to mark snapshot analysed: %w", err)
	}
	return tx.Commit()
}

func (s *sqliteStore) GetRecommendations(ctx context.Context, q RecommendationQuery) ([]*model.Recommendation, error) {
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, raw_data FROM recommendations WHERE 1 = 1`
	args := []interface{}{}
	if q.LatestOnly {
		query += ` AND snapshot_id = (SELECT MAX(id) FROM cluster_snapshots WHERE analyzed = 1)`
	}
	if q.Status != "" {
		query += ` AND status = ?`
		args = append(args, q.Status)
	}
	query += ` ORDER BY timestamp DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query recommendations: %w", err)
	}
	defer rows.Close()

	recommendations := make([]*model.Recommendation, 0)
	for rows.Next() {
		var id int64
		var rawData string
		if err := rows.Scan(&id, &rawData); err != nil {
			return nil, fmt.Errorf("failed to scan recommendation: %w", err)
		}
		var recommendation model.Recommendation
		if err := json.Unmarshal([]byte(rawData), &recommendation); err != nil {
			return nil, fmt.Errorf("failed to unmarshal recommendation: %w", err)
		}
		recommendation.ID = id
		recommendations = append(recommendations, &recommendation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recommendation rows error: %w", err)
	}
	return recommendations, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
