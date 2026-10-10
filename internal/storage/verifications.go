package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"watchdog-agent/internal/model"
)

const verificationSchema = `
	CREATE TABLE IF NOT EXISTS verifications (
		pr INTEGER PRIMARY KEY,
		target TEXT NOT NULL,
		status TEXT NOT NULL,
		merged_at DATETIME NOT NULL,
		raw_data TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_verifications_merged_at ON verifications(merged_at DESC);

	CREATE TABLE IF NOT EXISTS halted_workloads (
		target TEXT PRIMARY KEY,
		raw_data TEXT NOT NULL
	);
`

func (s *sqliteStore) SaveVerification(ctx context.Context, v *model.Verification) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal verification: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO verifications (pr, target, status, merged_at, raw_data)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(pr) DO UPDATE SET target = excluded.target, status = excluded.status,
			merged_at = excluded.merged_at, raw_data = excluded.raw_data`,
		v.PR, v.Target, v.Status, formatTimestamp(v.MergedAt), string(raw))
	if err != nil {
		return fmt.Errorf("failed to save verification for PR #%d: %w", v.PR, err)
	}
	return nil
}

func (s *sqliteStore) GetVerification(ctx context.Context, pr int) (*model.Verification, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT raw_data FROM verifications WHERE pr = ?`, pr).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query verification for PR #%d: %w", pr, err)
	}
	var v model.Verification
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("failed to unmarshal verification for PR #%d: %w", pr, err)
	}
	return &v, nil
}

func (s *sqliteStore) GetVerifications(ctx context.Context, limit int) ([]*model.Verification, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT raw_data FROM verifications ORDER BY merged_at DESC, pr DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query verifications: %w", err)
	}
	defer rows.Close()

	out := make([]*model.Verification, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("failed to scan verification: %w", err)
		}
		var v model.Verification
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("failed to unmarshal verification: %w", err)
		}
		out = append(out, &v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("verification rows error: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) SetHaltedWorkloads(ctx context.Context, halted []model.HaltedWorkload) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin halted workloads transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM halted_workloads`); err != nil {
		return fmt.Errorf("failed to clear halted workloads: %w", err)
	}
	for _, h := range halted {
		raw, err := json.Marshal(h)
		if err != nil {
			return fmt.Errorf("failed to marshal halted workload: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO halted_workloads (target, raw_data) VALUES (?, ?)`, h.Target, string(raw)); err != nil {
			return fmt.Errorf("failed to save halted workload %s: %w", h.Target, err)
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) GetHaltedWorkloads(ctx context.Context) ([]model.HaltedWorkload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT raw_data FROM halted_workloads ORDER BY target`)
	if err != nil {
		return nil, fmt.Errorf("failed to query halted workloads: %w", err)
	}
	defer rows.Close()

	out := make([]model.HaltedWorkload, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("failed to scan halted workload: %w", err)
		}
		var h model.HaltedWorkload
		if err := json.Unmarshal([]byte(raw), &h); err != nil {
			return nil, fmt.Errorf("failed to unmarshal halted workload: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("halted workload rows error: %w", err)
	}
	return out, nil
}
