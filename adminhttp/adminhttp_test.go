package adminhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sudarkoff/flagpole"
)

// memStore is an in-memory Store for testing.
type memStore struct {
	mu   sync.Mutex
	defs map[string]flagpole.Feature
}

func (m *memStore) List(context.Context) (map[string]flagpole.Feature, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]flagpole.Feature, len(m.defs))
	for k, v := range m.defs {
		cp[k] = v
	}
	return cp, nil
}

func (m *memStore) Upsert(_ context.Context, key string, f flagpole.Feature) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defs[key] = f
	return nil
}

func (m *memStore) Archive(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.defs, key)
	return nil
}

func TestUpsertThenList(t *testing.T) {
	store := &memStore{defs: map[string]flagpole.Feature{}}
	h := NewHandler(store)

	// Upsert
	body := `{"defaultValue": false, "rules": [{"force": true, "coverage": 0.5}]}`
	req := httptest.NewRequest(http.MethodPut, "/flags/new-flag", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status = %d", rec.Code)
	}

	// List
	req = httptest.NewRequest(http.MethodGet, "/flags", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "new-flag") {
		t.Errorf("list missing new-flag: %s", rec.Body.String())
	}
}
