package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/yourorg/reader/internal/domain"
)

type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type ProgressStore struct {
	db DB
}

// NewProgressStore wraps db as a ports.ProgressStore.
func NewProgressStore(db DB) *ProgressStore {
	return &ProgressStore{db: db}
}

func (s *ProgressStore) Save(ctx context.Context, state domain.FileState) error {
	const query = `
		INSERT INTO file_management
			(file_id, status, current_line, matched_count, selected_regex_id, priority, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (file_id) DO UPDATE SET
			status = EXCLUDED.status,
			current_line = EXCLUDED.current_line,
			matched_count = EXCLUDED.matched_count,
			selected_regex_id = EXCLUDED.selected_regex_id,
			priority = EXCLUDED.priority,
			updated_at = EXCLUDED.updated_at`

	_, err := s.db.ExecContext(ctx, query,
		state.FileID, state.Status, state.CurrentLine, state.MatchedCount,
		state.SelectedRegexID, state.Priority, state.CreatedAt, state.UpdatedAt)
	if err != nil {
		return fmt.Errorf("storage: saving file state for %q: %w", state.FileID, err)
	}
	return nil
}

func (s *ProgressStore) Load(ctx context.Context, fileID string) (domain.FileState, bool, error) {
	const query = `
		SELECT file_id, status, current_line, matched_count, selected_regex_id, priority, created_at, updated_at
		FROM file_management WHERE file_id = $1`

	var state domain.FileState
	err := s.db.QueryRowContext(ctx, query, fileID).Scan(
		&state.FileID, &state.Status, &state.CurrentLine, &state.MatchedCount,
		&state.SelectedRegexID, &state.Priority, &state.CreatedAt, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FileState{}, false, nil
	}
	if err != nil {
		return domain.FileState{}, false, fmt.Errorf("storage: loading file state for %q: %w", fileID, err)
	}
	return state, true, nil
}

func (s *ProgressStore) FindReading(ctx context.Context) (domain.FileState, bool, error) {
	const query = `
		SELECT file_id, status, current_line, matched_count, selected_regex_id, priority, created_at, updated_at
		FROM file_management WHERE status = $1 LIMIT 1`

	var state domain.FileState
	err := s.db.QueryRowContext(ctx, query, domain.FileStatusReading).Scan(
		&state.FileID, &state.Status, &state.CurrentLine, &state.MatchedCount,
		&state.SelectedRegexID, &state.Priority, &state.CreatedAt, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FileState{}, false, nil
	}
	if err != nil {
		return domain.FileState{}, false, fmt.Errorf("storage: finding reading file: %w", err)
	}
	return state, true, nil
}

// LookupStatuses reads the checkpoint fields exposed by the Status API.
func (s *ProgressStore) LookupStatuses(ctx context.Context, fileIDs []string) (map[string]domain.FileState, error) {
	const query = `
		SELECT file_id, status, current_line, matched_count
		FROM file_management WHERE file_id = ANY($1::text[])`

	rows, err := s.db.QueryContext(ctx, query, fileIDs)
	if err != nil {
		return nil, fmt.Errorf("storage: querying file statuses: %w", err)
	}
	defer rows.Close()

	states := make(map[string]domain.FileState, len(fileIDs))
	for rows.Next() {
		var state domain.FileState
		if err := rows.Scan(&state.FileID, &state.Status, &state.CurrentLine, &state.MatchedCount); err != nil {
			return nil, fmt.Errorf("storage: scanning file status: %w", err)
		}
		states[state.FileID] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterating file statuses: %w", err)
	}
	return states, nil
}

// RegexSource is the PostgreSQL-backed ports.RegexSource adapter,
// replacing the Phase 0 stub's hardcoded single pattern.
type RegexSource struct {
	db DB
}

// NewRegexSource wraps db as a ports.RegexSource.
func NewRegexSource(db DB) *RegexSource {
	return &RegexSource{db: db}
}

func (s *RegexSource) ActivePatterns(ctx context.Context) ([]domain.RegexPattern, error) {
	const query = `SELECT id, name, pattern, is_active FROM regex WHERE is_active = true`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("storage: loading active regex patterns: %w", err)
	}
	defer rows.Close()

	var patterns []domain.RegexPattern
	for rows.Next() {
		var p domain.RegexPattern
		if err := rows.Scan(&p.ID, &p.Name, &p.Pattern, &p.IsActive); err != nil {
			return nil, fmt.Errorf("storage: scanning regex pattern row: %w", err)
		}
		patterns = append(patterns, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterating regex pattern rows: %w", err)
	}
	return patterns, nil
}
