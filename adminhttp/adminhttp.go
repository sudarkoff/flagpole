// Package adminhttp exposes a mountable, auth-agnostic JSON CRUD handler for
// managing flagpole feature definitions. Wrap it with your own auth middleware.
package adminhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sudarkoff/flagpole"
)

// Store is the persistence the admin handler operates on. A Postgres-backed
// implementation typically lives next to your sourcepg setup.
//
// Archive should be idempotent: implementations should return nil even when the
// key does not exist (the handler responds 200 either way).
type Store interface {
	List(ctx context.Context) (map[string]flagpole.Feature, error)
	Upsert(ctx context.Context, key string, f flagpole.Feature) error
	Archive(ctx context.Context, key string) error
}

// NewHandler returns an http.Handler serving:
//
//	GET    /flags          -> {key: Feature}
//	PUT    /flags/{key}     -> upsert (body = Feature JSON)
//	DELETE /flags/{key}     -> archive
func NewHandler(store Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/flags", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defs, err := store.List(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, defs)
	})
	mux.HandleFunc("/flags/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/flags/")
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			var f flagpole.Feature
			if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := store.Upsert(r.Context(), key, f); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			if err := store.Archive(r.Context(), key); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
