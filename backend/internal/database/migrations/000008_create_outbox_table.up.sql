-- Transactional outbox: domain events are inserted here in the SAME transaction
-- as the state change that produced them, then dispatched after commit by the
-- relay. published_at IS NULL means "not yet dispatched".
CREATE TABLE IF NOT EXISTS outbox (
    id           BIGSERIAL PRIMARY KEY,
    event_type   TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

-- The relay repeatedly scans for undispatched rows in insertion order.
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
