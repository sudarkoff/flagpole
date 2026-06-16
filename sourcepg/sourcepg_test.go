package sourcepg

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("FLAGPOLE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("FLAGPOLE_TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	_, _ = pool.Exec(context.Background(), "TRUNCATE feature_flags")
	return pool
}

func TestSourceLoadSkipsArchived(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO feature_flags (key, definition, archived) VALUES
		 ('live', '{"defaultValue": true}', false),
		 ('dead', '{"defaultValue": true}', true)`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := New(pool)
	feats, err := src.Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := feats["live"]; !ok {
		t.Error("expected live flag")
	}
	if _, ok := feats["dead"]; ok {
		t.Error("archived flag must not load")
	}
}
