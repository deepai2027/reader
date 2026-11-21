//go:build integration

// Package integrationinfra starts isolated services for the tagged suites.
package integrationinfra

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/minio"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func PostgreSQL(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("reader"),
		postgres.WithUsername("reader"), postgres.WithPassword("reader"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

func MinIO(t *testing.T) (endpoint, accessKey, secretKey string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	server, err := minio.Run(ctx, "minio/minio:RELEASE.2024-01-16T16-07-38Z")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Terminate(context.Background()) })
	endpoint, err = server.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint, server.Username, server.Password
}
