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

// errStore is a Store whose Upsert always returns an error, used to exercise
// the 500 path in the PUT handler.
type errStore struct{}

func (e *errStore) List(_ context.Context) (map[string]flagpole.Feature, error) {
	return nil, nil
}

func (e *errStore) Upsert(_ context.Context, _ string, _ flagpole.Feature) error {
	return errors.New("store unavailable")
}

func (e *errStore) Archive(_ context.Context, _ string) error {
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

func TestDeleteArchives(t *testing.T) {
	store := &memStore{defs: map[string]flagpole.Feature{
		"to-delete": {DefaultValue: true},
	}}
	h := NewHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/flags/to-delete", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", rec.Code)
	}

	flags, err := store.List(req.Context())
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if _, exists := flags["to-delete"]; exists {
		t.Errorf("expected key 'to-delete' to be removed from store after DELETE")
	}
}

func TestGetOnSpecificKeyIs405(t *testing.T) {
	store := &memStore{defs: map[string]flagpole.Feature{}}
	h := NewHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/flags/somekey", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /flags/somekey status = %d, want 405", rec.Code)
	}
}

func TestPostFlagsIs405(t *testing.T) {
	store := &memStore{defs: map[string]flagpole.Feature{}}
	h := NewHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /flags status = %d, want 405", rec.Code)
	}
}

func TestPutEmptyKeyIs400(t *testing.T) {
	store := &memStore{defs: map[string]flagpole.Feature{}}
	h := NewHandler(store)

	req := httptest.NewRequest(http.MethodPut, "/flags/", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /flags/ status = %d, want 400", rec.Code)
	}
}

func TestBadJSONIs400(t *testing.T) {
	store := &memStore{defs: map[string]flagpole.Feature{}}
	h := NewHandler(store)

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
