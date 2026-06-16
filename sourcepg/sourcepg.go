// Package sourcepg provides a Postgres-backed flagpole.Source over a
// feature_flags table. See schema.sql for the expected DDL.
package sourcepg

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sudarkoff/flagpole"
)

// Source loads non-archived feature definitions from Postgres.
type Source struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Source { return &Source{pool: pool} }

func (s *Source) Load(ctx context.Context) (map[string]flagpole.Feature, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key, definition FROM feature_flags WHERE archived = false`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]flagpole.Feature)
	for rows.Next() {
		var key string
		var def []byte
		if err := rows.Scan(&key, &def); err != nil {
			return nil, err
		}
		var f flagpole.Feature
		if err := json.Unmarshal(def, &f); err != nil {
			return nil, err
		}
		out[key] = f
	}
	return out, rows.Err()
}

// compile-time check that *Source satisfies flagpole.Source.
var _ flagpole.Source = (*Source)(nil)
