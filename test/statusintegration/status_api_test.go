//go:build integration

package statusintegration

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/statusapi"
	"github.com/yourorg/reader/internal/storage"
	"github.com/yourorg/reader/migrations"
	"github.com/yourorg/reader/test/integrationinfra"
)

func TestStatusAPI_PostgreSQL(t *testing.T) {
	dsn := os.Getenv("READER_STATUS_TEST_DATABASE_DSN")
	if dsn == "" {
		dsn = integrationinfra.PostgreSQL(t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := migrations.Up(dsn); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	checkStatusAPI(t, ctx, storage.NewProgressStore(db))
}

func checkStatusAPI(t *testing.T, ctx context.Context, store *storage.ProgressStore) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	state := domain.FileState{FileID: "active", Status: domain.FileStatusReading, CurrentLine: 5, MatchedCount: 2, CreatedAt: now, UpdatedAt: now}
	done := domain.FileState{FileID: "done", Status: domain.FileStatusDone, CreatedAt: now, UpdatedAt: now}
	if err := store.Save(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, done); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(statusapi.New(store, "test-key", slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	checkResponse := func(want string) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/status", strings.NewReader(
			`{"file_ids":["done","active","unknown'file","active"]}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-API-Key", "test-key")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != want {
			t.Fatalf("got HTTP %d with %s, want %s", res.StatusCode, body, want)
		}
	}
	checkResponse(`{"files":[{"file_id":"done","state":"done","lines_read":0,"lines_accepted":0},{"file_id":"active","state":"reading","lines_read":5,"lines_accepted":2}]}`)
	state.CurrentLine, state.MatchedCount = 20, 12
	if err := store.Save(ctx, state); err != nil {
		t.Fatal(err)
	}
	checkResponse(`{"files":[{"file_id":"done","state":"done","lines_read":0,"lines_accepted":0},{"file_id":"active","state":"reading","lines_read":20,"lines_accepted":12}]}`)
	persisted, found, err := store.Load(ctx, state.FileID)
	if err != nil || !found || persisted.CurrentLine != 20 || persisted.MatchedCount != 12 || !persisted.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected state after lookup: %+v, found=%v, err=%v", persisted, found, err)
	}
}
