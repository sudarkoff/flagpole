package trackpg

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sudarkoff/flagpole"
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
	if _, err := pool.Exec(context.Background(), "TRUNCATE experiment_exposures"); err != nil {
		t.Fatalf("truncate (did you apply schema.sql?): %v", err)
	}
	return pool
}

func countRows(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM experiment_exposures").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestTrackBatchesAndFlushesOnClose(t *testing.T) {
	pool := testPool(t)
	tr := New(pool, WithBatchSize(100), WithFlushInterval(time.Hour)) // flush only on Close
	for i := 0; i < 5; i++ {
		tr.Track(context.Background(), flagpole.Exposure{
			ExperimentKey: "exp1",
			VariationID:   i % 2,
			HashAttribute: "id",
			HashValue:     "u",
			Attributes:    flagpole.Attributes{"plan": "pro"},
			At:            time.Now(),
		})
	}
	if err := tr.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if n := countRows(t, pool); n != 5 {
		t.Errorf("rows = %d, want 5", n)
	}
}

func TestTrackDropsOnOverflow(t *testing.T) {
	pool := testPool(t)
	// Tiny buffer, never flush until Close, so the queue overflows.
	tr := New(pool, WithBufferSize(2), WithBatchSize(1000), WithFlushInterval(time.Hour))
	const n = 2000
	for i := 0; i < n; i++ {
		tr.Track(context.Background(), flagpole.Exposure{
			ExperimentKey: "exp1", HashAttribute: "id", HashValue: "u", At: time.Now(),
		})
	}
	if err := tr.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	dropped := tr.Dropped()
	written := countRows(t, pool)
	// No-loss invariant: every exposure is either dropped-and-counted or written.
	if int(dropped)+written != n {
		t.Errorf("dropped(%d) + written(%d) = %d, want %d", dropped, written, int(dropped)+written, n)
	}
	// The overflow path must actually be exercised.
	if dropped == 0 {
		t.Error("expected some dropped exposures under overflow")
	}
}

func TestTrackAttributesRoundTrip(t *testing.T) {
	pool := testPool(t)
	tr := New(pool, WithBatchSize(10), WithFlushInterval(time.Hour))
	tr.Track(context.Background(), flagpole.Exposure{
		ExperimentKey: "exp1",
		VariationID:   1,
		HashAttribute: "id",
		HashValue:     "u1",
		Attributes:    flagpole.Attributes{"plan": "pro", "beta": true, "country": "US"},
		At:            time.Now(),
	})
	if err := tr.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	var raw []byte
	if err := pool.QueryRow(context.Background(),
		"SELECT attributes FROM experiment_exposures WHERE hash_value = 'u1'").Scan(&raw); err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal attributes: %v", err)
	}
	if got["plan"] != "pro" || got["beta"] != true || got["country"] != "US" {
		t.Errorf("attributes round-trip mismatch: %#v", got)
	}
}
