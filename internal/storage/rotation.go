package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yourorg/reader/internal/domain"
)

// RotationStore coordinates retention and processing using the same per-file
// advisory lock. The row lock also serializes updates to an eligible ledger row.
type RotationStore struct {
	db *sql.DB
}

func NewRotationStore(db *sql.DB) *RotationStore {
	return &RotationStore{db: db}
}

const tryFileLock = `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 721456003))`

// LockPolling grants one Reader ownership from assignment through processing.
func (s *RotationStore) LockPolling(ctx context.Context) (func(), error) {
	return s.tryLock(ctx, `SELECT pg_try_advisory_xact_lock(721456003, 1)`)
}

// LockProcessing holds the file lock while the pipeline reads and checkpoints
// the object. Checkpoints use their own connections and commit normally.
func (s *RotationStore) LockProcessing(ctx context.Context, fileID string) (func(), error) {
	return s.tryLock(ctx, tryFileLock, fileID)
}

func (s *RotationStore) tryLock(ctx context.Context, query string, args ...any) (func(), error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var acquired bool
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&acquired); err != nil || !acquired {
		_ = tx.Rollback()
		return nil, err
	}
	return func() { _ = tx.Rollback() }, nil
}

// PostgreSQL's interval arithmetic clamps the day to the end of the target
// month. Convert to UTC first so server timezone and DST play no part.
const expired = `((created_at AT TIME ZONE 'UTC') + INTERVAL '1 month') <= ($2::timestamptz AT TIME ZONE 'UTC')`

func (s *RotationStore) Candidates(ctx context.Context, after string, now time.Time, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT file_id FROM file_management
		WHERE status = $1 AND `+expired+` AND file_id > $3
		ORDER BY file_id LIMIT $4`, domain.FileStatusDone, now, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *RotationStore) RemoveExpired(ctx context.Context, fileID string, now time.Time, removeObject func(context.Context, string) error) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var acquired bool
	if err := tx.QueryRowContext(ctx, tryFileLock, fileID).Scan(&acquired); err != nil || !acquired {
		return false, err
	}

	var eligible string
	err = tx.QueryRowContext(ctx, `
		SELECT file_id FROM file_management
		WHERE status = $1 AND `+expired+` AND file_id = $3
		FOR UPDATE SKIP LOCKED`, domain.FileStatusDone, now, fileID).Scan(&eligible)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := removeObject(ctx, eligible); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM file_management WHERE file_id = $1`, eligible); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
