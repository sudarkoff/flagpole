package flagpole

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// flakySource is a Source whose Load can be made to fail after a given number
// of successful loads, for exercising the refresh loop's error handling.
type flakySource struct {
	mu        sync.Mutex
	loads     int
	failAfter int // once loads exceeds this (>0), Load returns an error
	feats     map[string]Feature
}

func (s *flakySource) Load(context.Context) (map[string]Feature, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	if s.failAfter > 0 && s.loads > s.failAfter {
		return nil, errors.New("boom")
	}
	return s.feats, nil
}

func TestClientLastRefreshSetAfterNew(t *testing.T) {
	c, err := New(context.Background(), StaticSource{Features: map[string]Feature{}},
		WithRefreshInterval(0))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if c.LastRefresh().IsZero() {
		t.Error("LastRefresh should be set after a successful initial load")
	}
}

func TestClientLastRefreshAdvancesOnRefresh(t *testing.T) {
	src := &flakySource{feats: map[string]Feature{}}
	c, err := New(context.Background(), src, WithRefreshInterval(5*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	t0 := c.LastRefresh()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.LastRefresh().After(t0) {
			return // advanced — success
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("LastRefresh did not advance after a successful background refresh")
}

func TestClientOnErrorCalledAndLastRefreshFrozenOnFailure(t *testing.T) {
	// First load (in New) succeeds; every subsequent refresh fails.
	src := &flakySource{failAfter: 1, feats: map[string]Feature{
		"f1": {DefaultValue: true},
	}}
	errs := make(chan error, 8)
	c, err := New(context.Background(), src,
		WithRefreshInterval(5*time.Millisecond),
		WithOnError(func(e error) { errs <- e }))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	t0 := c.LastRefresh()

	select {
	case <-errs:
		// good — the error hook fired on a failed refresh
	case <-time.After(2 * time.Second):
		t.Fatal("OnError was not called after a failing refresh")
	}

	// A failed refresh must not advance LastRefresh, and stale flags keep serving.
	if c.LastRefresh() != t0 {
		t.Errorf("LastRefresh advanced despite refresh failure: %v != %v", c.LastRefresh(), t0)
	}
	if !c.For(Attributes{"id": "u1"}).IsOn("f1") {
		t.Error("Client should keep serving the last good snapshot when refresh fails")
	}
}
