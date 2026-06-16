package flagpole

import (
	"context"
	"sync"
	"time"
)

// Client caches feature definitions from a Source and evaluates them locally.
// It is safe for concurrent use.
type Client struct {
	src     Source
	tracker Tracker

	mu          sync.RWMutex
	features    map[string]Feature
	lastRefresh time.Time

	refresh time.Duration
	onError func(error)
	cancel  context.CancelFunc
	done    chan struct{}
}

// Option configures a Client.
type Option func(*Client)

// WithRefreshInterval sets how often the Client reloads from its Source.
// Zero or negative disables background refresh (load once).
func WithRefreshInterval(d time.Duration) Option {
	return func(c *Client) { c.refresh = d }
}

// WithTracker sets the experiment exposure tracker (default: NoopTracker).
func WithTracker(tr Tracker) Option {
	return func(c *Client) { c.tracker = tr }
}

// WithOnError sets a callback invoked when a background refresh fails. The
// Client keeps serving the last good snapshot regardless; the hook exists so
// hosts can surface the failure (log/metric/alert) instead of it being silent.
// The default is a no-op. Keep the callback cheap and non-blocking — it runs on
// the refresh goroutine and delays the next reload.
func WithOnError(fn func(error)) Option {
	return func(c *Client) {
		if fn != nil {
			c.onError = fn
		}
	}
}

// New loads features once synchronously, then (unless disabled) refreshes them
// on an interval until Close is called.
func New(ctx context.Context, src Source, opts ...Option) (*Client, error) {
	c := &Client{
		src:     src,
		tracker: NoopTracker{},
		refresh: 60 * time.Second,
		onError: func(error) {},
		done:    make(chan struct{}),
	}
	for _, o := range opts {
		o(c)
	}
	if err := c.reload(ctx); err != nil {
		return nil, err
	}
	if c.refresh > 0 {
		var bg context.Context
		bg, c.cancel = context.WithCancel(context.Background())
		go c.loop(bg)
	} else {
		close(c.done)
	}
	return c, nil
}

func (c *Client) reload(ctx context.Context) error {
	feats, err := c.src.Load(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.features = feats
	c.lastRefresh = time.Now()
	c.mu.Unlock()
	return nil
}

// LastRefresh returns the time of the most recent successful load from the
// Source (the initial load in New, or a background refresh). Hosts can alert
// when time.Since(LastRefresh()) exceeds a multiple of the refresh interval to
// detect a Client stuck serving stale flags after repeated refresh failures.
func (c *Client) LastRefresh() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastRefresh
}

func (c *Client) loop(ctx context.Context) {
	defer close(c.done)
	t := time.NewTicker(c.refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.reload(ctx); err != nil {
				c.onError(err) // keep serving stale; surface the failure
			}
		}
	}
}

// Close stops background refresh.
func (c *Client) Close() {
	if c.cancel != nil {
		c.cancel()
	}
	<-c.done
}

// For binds attributes for evaluation.
func (c *Client) For(attrs Attributes) *Evaluation {
	return &Evaluation{client: c, attrs: attrs}
}

// Evaluation evaluates flags for a fixed set of attributes.
type Evaluation struct {
	client *Client
	attrs  Attributes
}

func (e *Evaluation) result(key string) Result {
	e.client.mu.RLock()
	feat, ok := e.client.features[key]
	e.client.mu.RUnlock()
	if !ok {
		return Result{Value: nil, On: false}
	}
	return Evaluate(feat, key, e.attrs)
}

// IsOn reports whether the flag resolves to a truthy value. Unknown flags are off.
func (e *Evaluation) IsOn(key string) bool { return e.result(key).On }

// Value returns the flag's resolved value, or def if the flag is unknown or
// resolves to nil. An unknown flag yields a nil Result value, so a single
// evaluation covers both cases.
func (e *Evaluation) Value(key string, def any) any {
	if v := e.result(key).Value; v != nil {
		return v
	}
	return def
}
