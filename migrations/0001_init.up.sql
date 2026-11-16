-- Reader schema: Final Phase Step A (specs/2026-11-10-mvp/plan.md
-- Group 5). Mirrors internal/domain's FileState and RegexPattern.
CREATE TABLE regex (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    pattern TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE file_management (
    file_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    current_line BIGINT NOT NULL DEFAULT 0,
    matched_count BIGINT NOT NULL DEFAULT 0,
    selected_regex_id BIGINT REFERENCES regex(id),
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- Supports ProgressStore.FindReading's WHERE status = 'reading' scan.
CREATE INDEX idx_file_management_status ON file_management (status);
