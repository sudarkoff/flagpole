package adminhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sudarkoff/flagpole"
)

// memStore is an in-memory Store for testing. Archived records move to the
// archived map; Restore moves them back.
type memStore struct {
	mu       sync.Mutex
	defs     map[string]Record
	archived map[string]Record
}

func newMemStore() *memStore {
	return &memStore{defs: map[string]Record{}, archived: map[string]Record{}}
}

func (m *memStore) List(context.Context) (map[string]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]Record, len(m.defs))
	for k, v := range m.defs {
		cp[k] = v
	}
	return cp, nil
}

func (m *memStore) ListArchived(context.Context) (map[string]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]Record, len(m.archived))
	for k, v := range m.archived {
		cp[k] = v
	}
	return cp, nil
}

func (m *memStore) Upsert(_ context.Context, key string, r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.Archived = false
	m.defs[key] = r
	delete(m.archived, key)
	return nil
}

func (m *memStore) Archive(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.defs[key]; ok {
		r.Archived = true
		m.archived[key] = r
		delete(m.defs, key)
	}
	return nil
}

func (m *memStore) Restore(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.archived[key]; ok {
		r.Archived = false
		m.defs[key] = r
		delete(m.archived, key)
	}
	return nil
}

// errStore is a Store whose Upsert always returns an error, used to exercise
// the 500 path in the PUT handler.
type errStore struct{}

func (e *errStore) List(_ context.Context) (map[string]Record, error)         { return nil, nil }
func (e *errStore) ListArchived(_ context.Context) (map[string]Record, error) { return nil, nil }
func (e *errStore) Upsert(_ context.Context, _ string, _ Record) error {
	return errors.New("store unavailable")
}
func (e *errStore) Archive(_ context.Context, _ string) error { return nil }
func (e *errStore) Restore(_ context.Context, _ string) error { return nil }

func TestUpsertThenList(t *testing.T) {
	store := newMemStore()
	h := NewHandler(store)

	// Upsert with a description alongside the definition.
	body := `{"defaultValue": false, "rules": [{"force": true, "coverage": 0.5}], "description": "rollout test"}`
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
	// The description rides along in the response (superset of bare Feature).
	if !strings.Contains(rec.Body.String(), "rollout test") {
		t.Errorf("list missing description: %s", rec.Body.String())
	}
}

func TestDeleteArchivesThenRestore(t *testing.T) {
	store := newMemStore()
	store.defs["to-delete"] = Record{Feature: flagpole.Feature{DefaultValue: true}}
	h := NewHandler(store)

	// DELETE archives.
	req := httptest.NewRequest(http.MethodDelete, "/flags/to-delete", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", rec.Code)
	}
	flags, _ := store.List(req.Context())
	if _, exists := flags["to-delete"]; exists {
		t.Errorf("expected 'to-delete' removed from active list after DELETE")
	}
	arch, _ := store.ListArchived(req.Context())
	if _, ok := arch["to-delete"]; !ok {
		t.Errorf("expected 'to-delete' to appear in archived list after DELETE")
	}

	// Archived list endpoint surfaces it.
	req = httptest.NewRequest(http.MethodGet, "/flags?archived=true", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "to-delete") {
		t.Fatalf("archived list status=%d body=%s", rec.Code, rec.Body.String())
	}

	// POST /flags/{key}/restore brings it back.
	req = httptest.NewRequest(http.MethodPost, "/flags/to-delete/restore", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status = %d, want 200", rec.Code)
	}
	flags, _ = store.List(req.Context())
	if _, ok := flags["to-delete"]; !ok {
		t.Errorf("expected 'to-delete' back in active list after restore")
	}
}

func TestRestoreWrongMethodIs405(t *testing.T) {
	h := NewHandler(newMemStore())
	req := httptest.NewRequest(http.MethodGet, "/flags/x/restore", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET restore status = %d, want 405", rec.Code)
	}
}

func TestGetOnSpecificKeyIs405(t *testing.T) {
	h := NewHandler(newMemStore())
	req := httptest.NewRequest(http.MethodGet, "/flags/somekey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /flags/somekey status = %d, want 405", rec.Code)
	}
}

func TestPostFlagsIs405(t *testing.T) {
	h := NewHandler(newMemStore())
	req := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /flags status = %d, want 405", rec.Code)
	}
}

func TestPutEmptyKeyIs400(t *testing.T) {
	h := NewHandler(newMemStore())
	req := httptest.NewRequest(http.MethodPut, "/flags/", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /flags/ status = %d, want 400", rec.Code)
	}
}

func TestBadJSONIs400(t *testing.T) {
	h := NewHandler(newMemStore())
	req := httptest.NewRequest(http.MethodPut, "/flags/x", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status = %d, want 400", rec.Code)
	}
}

func TestStoreErrorSurfaces500(t *testing.T) {
	h := NewHandler(&errStore{})
	body := `{"defaultValue": false}`
	req := httptest.NewRequest(http.MethodPut, "/flags/x", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error status = %d, want 500", rec.Code)
	}
}
