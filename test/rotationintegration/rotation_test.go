//go:build integration

package rotationintegration

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/logrotation"
	"github.com/yourorg/reader/internal/objectstore"
	"github.com/yourorg/reader/internal/storage"
)

func TestPostgreSQLRetention(t *testing.T) {
	ctx, db := testDatabase(t)
	ledger := storage.NewProgressStore(db)
	store := storage.NewRotationStore(db)
	for _, tc := range []struct {
		name, registered, expires string
	}{
		{"common-year", "2025-01-31T12:00:00Z", "2025-02-28T12:00:00Z"},
		{"leap-year", "2024-01-31T12:00:00Z", "2024-02-29T12:00:00Z"},
		{"year-boundary", "2025-12-31T12:00:00Z", "2026-01-31T12:00:00Z"},
		{"utc-date", "2025-03-01T00:30:00+01:00", "2025-03-28T23:30:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registered := timestamp(t, tc.registered)
			expires := timestamp(t, tc.expires)
			noonInOtherZone := expires.In(time.FixedZone("test", 7*60*60))
			for _, offset := range []time.Duration{-time.Microsecond, 0, time.Microsecond} {
				now := noonInOtherZone.Add(offset)
				seed(t, ctx, ledger, tc.name, domain.FileStatusDone, registered)
				ids, err := store.Candidates(ctx, "", now, 100)
				if err != nil {
					t.Fatal(err)
				}
				if (len(ids) == 1 && ids[0] == tc.name) != (offset >= 0) || len(ids) > 1 {
					t.Fatalf("at offset %s got candidates %v", offset, ids)
				}
				deleted, err := store.RemoveExpired(ctx, tc.name, now, func(context.Context, string) error { return nil })
				if err != nil || deleted != (offset >= 0) {
					t.Fatalf("at offset %s got deleted=%v, error=%v", offset, deleted, err)
				}
			}
		})
	}

	now := time.Now()
	old := now.AddDate(0, -2, 0)
	seed(t, ctx, ledger, "protected", domain.FileStatusReading, old)
	seed(t, ctx, ledger, "unknown", "queued", old)
	seed(t, ctx, ledger, "race", domain.FileStatusDone, old)
	t.Cleanup(func() { removeRows(t, db, "protected", "unknown", "race", "stale", "retry") })
	mustNotRemove := func(context.Context, string) error {
		t.Error("unexpected object removal")
		return errors.New("unexpected removal")
	}
	for _, id := range []string{"protected", "unknown"} {
		deleted, err := store.RemoveExpired(ctx, id, now, mustNotRemove)
		if deleted || err != nil {
			t.Fatalf("protected %s: deleted=%v, error=%v", id, deleted, err)
		}
	}

	release, err := store.LockProcessing(ctx, "race")
	if err != nil || release == nil {
		t.Fatalf("lock processing: acquired=%v, error=%v", release != nil, err)
	}
	deleted, err := store.RemoveExpired(ctx, "race", now, mustNotRemove)
	release()
	if deleted || err != nil {
		t.Fatalf("cleanup during processing: deleted=%v, error=%v", deleted, err)
	}

	deleted, err = store.RemoveExpired(ctx, "race", now, func(ctx context.Context, id string) error {
		release, err := store.LockProcessing(ctx, id)
		if release != nil {
			release()
			t.Error("processing acquired file during cleanup")
		}
		if err != nil {
			return err
		}
		removed, err := store.RemoveExpired(ctx, id, now, mustNotRemove)
		if removed || err != nil {
			t.Errorf("competing cleanup: deleted=%v, error=%v", removed, err)
		}
		return nil
	})
	if !deleted || err != nil {
		t.Fatalf("owned cleanup: deleted=%v, error=%v", deleted, err)
	}

	seed(t, ctx, ledger, "stale", domain.FileStatusDone, old)
	ids, err := store.Candidates(ctx, "", now, 100)
	if err != nil || len(ids) != 1 || ids[0] != "stale" {
		t.Fatalf("expected stale candidate in snapshot, got %v, error=%v", ids, err)
	}
	seed(t, ctx, ledger, "stale", domain.FileStatusReading, old)
	deleted, err = store.RemoveExpired(ctx, "stale", now, mustNotRemove)
	if deleted || err != nil {
		t.Fatalf("stale candidate: deleted=%v, error=%v", deleted, err)
	}

	testRetry(t, ctx, db, func(context.Context, string) error { return nil }, func() {})
}

func TestMinIORetention(t *testing.T) {
	endpoint := os.Getenv("READER_ROTATION_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("set READER_ROTATION_TEST_MINIO_ENDPOINT for the real MinIO integration checks")
	}
	ctx, db := testDatabase(t)
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv("READER_ROTATION_TEST_MINIO_ACCESS_KEY"), os.Getenv("READER_ROTATION_TEST_MINIO_SECRET_KEY"), ""),
		Secure: os.Getenv("READER_ROTATION_TEST_MINIO_USE_SSL") == "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	bucket := os.Getenv("READER_ROTATION_TEST_MINIO_BUCKET")
	if bucket == "" {
		t.Fatal("set READER_ROTATION_TEST_MINIO_BUCKET to a dedicated empty test bucket")
	}
	remove := objectstore.ObjectRemover(client, bucket)
	put := func(id string) {
		t.Helper()
		_, err := client.PutObject(ctx, bucket, id, strings.NewReader("test"), 4, minio.PutObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.RemoveObject(context.Background(), bucket, id, minio.RemoveObjectOptions{}) })
	}
	put("retry")
	testRetry(t, ctx, db, remove, func() { assertAbsent(t, ctx, client, bucket, "retry") })

	ledger := storage.NewProgressStore(db)
	store := storage.NewRotationStore(db)
	t.Cleanup(func() { removeRows(t, db, "retry", "scheduled-expired", "scheduled-fresh", "scheduled-reading") })
	now := time.Now()
	old := now.AddDate(0, -2, 0)
	for _, id := range []string{"scheduled-expired", "scheduled-fresh", "scheduled-reading"} {
		put(id)
	}
	seed(t, ctx, ledger, "scheduled-expired", domain.FileStatusDone, old)
	seed(t, ctx, ledger, "scheduled-fresh", domain.FileStatusDone, now)
	seed(t, ctx, ledger, "scheduled-reading", domain.FileStatusReading, old)

	schedulerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		logrotation.Run(schedulerCtx, 50*time.Millisecond, store, remove, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	defer func() { cancel(); <-done }()
	for deadline := time.Now().Add(5 * time.Second); ; {
		_, found, err := ledger.Load(ctx, "scheduled-expired")
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler did not remove the expired file")
		}
		time.Sleep(25 * time.Millisecond)
	}
	assertAbsent(t, ctx, client, bucket, "scheduled-expired")
	for _, id := range []string{"scheduled-fresh", "scheduled-reading"} {
		assertRow(t, ctx, ledger, id, true)
		if _, err := client.StatObject(ctx, bucket, id, minio.StatObjectOptions{}); err != nil {
			t.Fatalf("protected object %q: %v", id, err)
		}
	}
}

func testRetry(t *testing.T, ctx context.Context, db *sql.DB, remove func(context.Context, string) error, assertRemoved func()) {
	t.Helper()
	ledger := storage.NewProgressStore(db)
	store := storage.NewRotationStore(db)
	now := time.Now()
	seed(t, ctx, ledger, "retry", domain.FileStatusDone, now.AddDate(0, -2, 0))
	failure := errors.New("ambiguous MinIO failure")
	deleted, err := store.RemoveExpired(ctx, "retry", now, func(context.Context, string) error { return failure })
	if deleted || !errors.Is(err, failure) {
		t.Fatalf("storage failure: deleted=%v, error=%v", deleted, err)
	}
	assertRow(t, ctx, ledger, "retry", true)

	// Reject finalization after the object has been removed.
	if _, err := db.ExecContext(ctx, `
		CREATE FUNCTION rotation_test_reject_delete() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'injected database failure';
		END $$;
		CREATE TRIGGER rotation_test_reject_delete BEFORE DELETE ON file_management
		FOR EACH ROW WHEN (OLD.file_id = 'retry') EXECUTE FUNCTION rotation_test_reject_delete();
	`); err != nil {
		t.Fatal(err)
	}
	dropTrigger := func() {
		t.Helper()
		if _, err := db.ExecContext(context.Background(), `
			DROP TRIGGER IF EXISTS rotation_test_reject_delete ON file_management;
			DROP FUNCTION IF EXISTS rotation_test_reject_delete();
		`); err != nil {
			t.Error(err)
		}
	}
	defer dropTrigger()
	deleted, err = store.RemoveExpired(ctx, "retry", now, func(ctx context.Context, id string) error {
		assertRow(t, ctx, ledger, id, true)
		return remove(ctx, id)
	})
	if deleted || err == nil || !strings.Contains(err.Error(), "injected database failure") {
		t.Fatalf("database failure: deleted=%v, error=%v", deleted, err)
	}
	dropTrigger()
	assertRemoved()
	assertRow(t, ctx, ledger, "retry", true)
	deleted, err = store.RemoveExpired(ctx, "retry", now, remove)
	if !deleted || err != nil {
		t.Fatalf("retry: deleted=%v, error=%v", deleted, err)
	}
	assertRow(t, ctx, ledger, "retry", false)
}

func testDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("READER_ROTATION_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set READER_ROTATION_TEST_DATABASE_DSN to a dedicated empty test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	migrations, err := migrate.New("file://../../migrations", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		_, _ = migrations.Close()
		t.Fatal(err)
	}
	_, _ = migrations.Close()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return ctx, db
}

func removeRows(t *testing.T, db *sql.DB, ids ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `DELETE FROM file_management WHERE file_id = ANY($1::text[])`, ids); err != nil {
		t.Error(err)
	}
}

func timestamp(t *testing.T, value string) time.Time {
	t.Helper()
	result, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func seed(t *testing.T, ctx context.Context, ledger *storage.ProgressStore, id string, status domain.FileStatus, created time.Time) {
	t.Helper()
	if err := ledger.Save(ctx, domain.FileState{FileID: id, Status: status, CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatal(err)
	}
}

func assertRow(t *testing.T, ctx context.Context, ledger *storage.ProgressStore, id string, want bool) {
	t.Helper()
	_, found, err := ledger.Load(ctx, id)
	if err != nil || found != want {
		t.Fatalf("row %s: found=%v, want %v, error=%v", id, found, want, err)
	}
}

func assertAbsent(t *testing.T, ctx context.Context, client *minio.Client, bucket, id string) {
	t.Helper()
	_, err := client.StatObject(ctx, bucket, id, minio.StatObjectOptions{})
	if minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatalf("expected object %q to be absent, got %v", id, err)
	}
}
