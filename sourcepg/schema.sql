-- Reference DDL. Consumers run this via their own migration tooling.
CREATE TABLE IF NOT EXISTS feature_flags (
    key         TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    definition  JSONB NOT NULL,
    archived    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
