-- Append-only event log: a durable, per-stream history of domain events used for
-- replay (e.g. catching a reconnecting character up on what happened since it was
-- last active). Unlike the outbox (a transient dispatch queue), rows here are
-- never removed. Written in the same transaction as the state change.
CREATE TABLE IF NOT EXISTS event_log (
    id          BIGSERIAL   PRIMARY KEY,
    stream      TEXT        NOT NULL,
    event_type  TEXT        NOT NULL,
    payload     JSONB       NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Replay scans one stream from a point in time in insertion order.
CREATE INDEX IF NOT EXISTS idx_event_log_stream ON event_log (stream, id);
