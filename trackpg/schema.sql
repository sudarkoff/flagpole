-- Canonical exposures table written by trackpg and read by downstream BI
-- (e.g. gnomon). This file is the contract: column names and types here are the
-- single source of truth. Running this DDL/migration is the consumer's job
-- (same stance as sourcepg).
CREATE TABLE IF NOT EXISTS experiment_exposures (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    experiment_key TEXT        NOT NULL,
    variation_id   INTEGER     NOT NULL,
    hash_attribute TEXT        NOT NULL,
    hash_value     TEXT        NOT NULL,
    attributes     JSONB       NOT NULL DEFAULT '{}',
    exposed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_experiment_exposures_key_time
    ON experiment_exposures (experiment_key, exposed_at);
