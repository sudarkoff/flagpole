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

// Record is a feature definition plus its admin metadata. The embedded Feature
// keeps the JSON a superset of a bare Feature — defaultValue/rules sit at the
// top level alongside description/archived — so consumers that only read the
// evaluation shape keep working unchanged.
type Record struct {
	flagpole.Feature
	Description string `json:"description,omitempty"`
	Archived    bool   `json:"archived,omitempty"`
}

// Store is the persistence the admin handler operates on. A Postgres-backed
// implementation typically lives next to your sourcepg setup.
//
// Archive and Restore should be idempotent: implementations should return nil
// even when the key does not exist (the handler responds 200 either way).
type Store interface {
	List(ctx context.Context) (map[string]Record, error)
	ListArchived(ctx context.Context) (map[string]Record, error)
	Upsert(ctx context.Context, key string, r Record) error
	Archive(ctx context.Context, key string) error
	Restore(ctx context.Context, key string) error
}

// NewHandler returns an http.Handler serving:
//
//	GET    /flags                 -> {key: Record}  (active)
//	GET    /flags?archived=true    -> {key: Record}  (archived)
//	PUT    /flags/{key}            -> upsert (body = Record JSON; archived ignored)
//	DELETE /flags/{key}            -> archive
//	POST   /flags/{key}/restore    -> restore an archived flag
func NewHandler(store Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/flags", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		list := store.List
		if r.URL.Query().Get("archived") == "true" {
			list = store.ListArchived
		}
		defs, err := list(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, defs)
	})
	mux.HandleFunc("/flags/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/flags/")

		// POST /flags/{key}/restore
		if key, ok := strings.CutSuffix(rest, "/restore"); ok {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if key == "" {
				http.Error(w, "missing key", http.StatusBadRequest)
				return
			}
			if err := store.Restore(r.Context(), key); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		key := rest
		if key == "" {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			var rec Record
			if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := store.Upsert(r.Context(), key, rec); err != nil {
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
