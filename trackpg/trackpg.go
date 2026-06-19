// Package trackpg provides a Postgres-backed, async-batching flagpole.Tracker
// over an experiment_exposures table. See schema.sql for the canonical DDL —
// that schema is the contract downstream readers (e.g. gnomon) depend on.
package trackpg

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sudarkoff/flagpole"
)

// Tracker batches exposures and writes them to Postgres on a background
// goroutine. Track never blocks: when the buffer is full, exposures are dropped
// and counted (analytics data must never sit in the evaluation hot path).
type Tracker struct {
	pool          *pgxpool.Pool
	table         string
	batchSize     int
	flushInterval time.Duration
	onError       func(error)

	ch      chan flagpole.Exposure
	dropped atomic.Int64

	closeOnce sync.Once
	done      chan struct{}
	cancel    context.CancelFunc
}

// Option configures a Tracker.
type Option func(*Tracker)

// WithBatchSize sets the max rows per INSERT (default 100).
func WithBatchSize(n int) Option {
	return func(t *Tracker) {
		if n > 0 {
			t.batchSize = n
		}
	}
}

// WithFlushInterval sets the max delay before a partial batch is written
// (default 2s).
func WithFlushInterval(d time.Duration) Option {
	return func(t *Tracker) {
		if d > 0 {
			t.flushInterval = d
		}
	}
}

// WithBufferSize sets the in-memory queue depth (default 10000). When full,
// Track drops and counts the exposure.
func WithBufferSize(n int) Option {
	return func(t *Tracker) {
		if n > 0 {
			t.ch = make(chan flagpole.Exposure, n)
		}
	}
}

// WithTable overrides the table name (default "experiment_exposures"). The
// column contract is fixed by schema.sql.
func WithTable(name string) Option {
	return func(t *Tracker) {
		if name != "" {
			t.table = name
		}
	}
}

// WithOnError sets a callback invoked when a batch INSERT fails. Keep it cheap
// and non-blocking; it runs on the writer goroutine.
func WithOnError(fn func(error)) Option {
	return func(t *Tracker) {
		if fn != nil {
			t.onError = fn
		}
	}
}

var _ flagpole.Tracker = (*Tracker)(nil)

// New starts a Tracker and its background writer goroutine.
func New(pool *pgxpool.Pool, opts ...Option) *Tracker {
	t := &Tracker{
		pool:          pool,
		table:         "experiment_exposures",
		batchSize:     100,
		flushInterval: 2 * time.Second,
		onError:       func(error) {},
		ch:            make(chan flagpole.Exposure, 10000),
		done:          make(chan struct{}),
	}
	for _, o := range opts {
		o(t)
	}
	var ctx context.Context
	ctx, t.cancel = context.WithCancel(context.Background())
	go t.loop(ctx)
	return t
}

// Track enqueues an exposure. Non-blocking: a full buffer drops and counts it.
func (t *Tracker) Track(_ context.Context, e flagpole.Exposure) {
	select {
	case t.ch <- e:
	default:
		t.dropped.Add(1)
	}
}

// Dropped returns the number of exposures dropped due to a full buffer.
func (t *Tracker) Dropped() int64 { return t.dropped.Load() }

// Close stops accepting new exposures, flushes what is queued, and waits for the
// writer goroutine to finish.
func (t *Tracker) Close(ctx context.Context) error {
	t.closeOnce.Do(func() { t.cancel() })
	select {
	case <-t.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Tracker) loop(ctx context.Context) {
	defer close(t.done)
	ticker := time.NewTicker(t.flushInterval)
	defer ticker.Stop()
	batch := make([]flagpole.Exposure, 0, t.batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := t.write(context.Background(), batch); err != nil {
			t.onError(err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// Drain whatever is buffered, then flush and return.
			for {
				select {
				case e := <-t.ch:
					batch = append(batch, e)
					if len(batch) >= t.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case e := <-t.ch:
			batch = append(batch, e)
			if len(batch) >= t.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (t *Tracker) write(ctx context.Context, batch []flagpole.Exposure) error {
	rows := make([][]any, len(batch))
	for i, e := range batch {
		attrs, err := json.Marshal(e.Attributes)
		if err != nil {
			attrs = []byte("{}")
		}
		at := e.At
		if at.IsZero() {
			at = time.Now()
		}
		rows[i] = []any{e.ExperimentKey, e.VariationID, e.HashAttribute, e.HashValue, attrs, at}
	}
	_, err := t.pool.CopyFrom(ctx,
		pgx.Identifier{t.table},
		[]string{"experiment_key", "variation_id", "hash_attribute", "hash_value", "attributes", "exposed_at"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("trackpg: write batch of %d: %w", len(batch), err)
	}
	return nil
}
