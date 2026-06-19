package flagpole

import (
	"context"
	"sync"
	"testing"
)

type recordingTracker struct {
	mu  sync.Mutex
	exp []Exposure
}

func (r *recordingTracker) Track(_ context.Context, e Exposure) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exp = append(r.exp, e)
}

func (r *recordingTracker) all() []Exposure {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Exposure(nil), r.exp...)
}

func newExpClient(t *testing.T, tr Tracker) *Client {
	t.Helper()
	src := StaticSource{Features: map[string]Feature{
		"feat": {
			DefaultValue: "control",
			Rules: []Rule{{
				Key:        "exp1",
				Variations: []any{"a", "b"},
			}},
		},
	}}
	c, err := New(context.Background(), src, WithRefreshInterval(0), WithTracker(tr))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestExposureFiresOncePerBinding(t *testing.T) {
	tr := &recordingTracker{}
	c := newExpClient(t, tr)
	ev := c.For(Attributes{"id": "user-1"})
	_ = ev.IsOn("feat")
	_ = ev.Value("feat", nil) // second read of same key, same binding
	got := tr.all()
	if len(got) != 1 {
		t.Fatalf("expected 1 exposure, got %d", len(got))
	}
	e := got[0]
	if e.ExperimentKey != "exp1" || e.HashAttribute != "id" || e.HashValue != "user-1" {
		t.Errorf("exposure = %+v", e)
	}
	if e.At.IsZero() {
		t.Error("exposure timestamp not set")
	}
}

func TestExposureDoesNotFireWhenNotInExperiment(t *testing.T) {
	tr := &recordingTracker{}
	c := newExpClient(t, tr)
	// No identifier => not in experiment => no exposure.
	_ = c.For(Attributes{"plan": "pro"}).IsOn("feat")
	if n := len(tr.all()); n != 0 {
		t.Errorf("expected 0 exposures, got %d", n)
	}
}

func TestForContextPropagates(t *testing.T) {
	type ctxKey struct{}
	var seen any
	tr := trackerFunc(func(ctx context.Context, _ Exposure) { seen = ctx.Value(ctxKey{}) })
	c := newExpClient(t, tr)
	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	_ = c.ForContext(ctx, Attributes{"id": "user-1"}).IsOn("feat")
	if seen != "v" {
		t.Errorf("context value not propagated to Track: %v", seen)
	}
}

type trackerFunc func(context.Context, Exposure)

func (f trackerFunc) Track(ctx context.Context, e Exposure) { f(ctx, e) }

func TestExposureFiresOnceUnderConcurrency(t *testing.T) {
	tr := &recordingTracker{}
	c := newExpClient(t, tr)
	ev := c.For(Attributes{"id": "user-1"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = ev.IsOn("feat") }()
	}
	wg.Wait()
	if n := len(tr.all()); n != 1 {
		t.Fatalf("expected exactly 1 exposure under concurrency, got %d", n)
	}
}

func TestExposureUsesMatchedExperimentRuleKey(t *testing.T) {
	src := StaticSource{Features: map[string]Feature{
		"feat": {
			DefaultValue: "control",
			Rules: []Rule{
				{Key: "exp-a", Condition: map[string]any{"plan": "enterprise"}, Variations: []any{"a", "b"}},
				{Key: "exp-b", Variations: []any{"a", "b"}},
			},
		},
	}}
	tr := &recordingTracker{}
	c, err := New(context.Background(), src, WithRefreshInterval(0), WithTracker(tr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	// attrs has plan=free, so exp-a's condition (plan=enterprise) is not met;
	// falls through to exp-b which has no condition => matches.
	_ = c.For(Attributes{"id": "u1", "plan": "free"}).IsOn("feat")
	got := tr.all()
	if len(got) != 1 || got[0].ExperimentKey != "exp-b" {
		t.Fatalf("expected one exposure with ExperimentKey=exp-b, got %+v", got)
	}
}
