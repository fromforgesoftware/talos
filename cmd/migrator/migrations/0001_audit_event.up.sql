CREATE TABLE talos.audit_event (
    id            UUID NOT NULL DEFAULT uuid_generate_v4(),
    timestamp     TIMESTAMPTZ NOT NULL DEFAULT now(),
    realm_id      UUID,
    actor_id      TEXT,
    actor_type    TEXT,
    resource_type TEXT,
    resource_id   TEXT,
    action        TEXT NOT NULL,
    summary       TEXT,
    changes       JSONB,
    metadata      JSONB,
    ip            TEXT,
    request_id    TEXT,
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);

-- Bootstrap partitions. From here on the application's partition-maintenance
-- routine (internal/app.PartitionProvisioner, run at startup and on the 6h
-- maintenance ticker in internal/module.go) creates each upcoming month's
-- partition before events for it arrive, so rows never fall into the DEFAULT
-- catch-all in normal operation.
CREATE TABLE talos.audit_event_2026_05 PARTITION OF talos.audit_event
    FOR VALUES FROM ('2026-05-01 00:00:00+00') TO ('2026-06-01 00:00:00+00');
CREATE TABLE talos.audit_event_2026_06 PARTITION OF talos.audit_event
    FOR VALUES FROM ('2026-06-01 00:00:00+00') TO ('2026-07-01 00:00:00+00');

-- DEFAULT catch-all retained as a safety net for any event that races ahead of
-- the maintenance routine (e.g. an event for month N+2). Such rows would NOT be
-- dropped by retention (it only matches audit_event_YYYY_MM), so operators
-- should alert on a non-empty audit_event_default and drain it into the correct
-- monthly partition.
CREATE TABLE talos.audit_event_default PARTITION OF talos.audit_event DEFAULT;

CREATE INDEX idx_audit_event_metadata ON talos.audit_event USING GIN (metadata);
CREATE INDEX idx_audit_event_actor ON talos.audit_event (actor_id, timestamp);
CREATE INDEX idx_audit_event_resource ON talos.audit_event (resource_type, resource_id, timestamp);
CREATE INDEX idx_audit_event_action ON talos.audit_event (action, timestamp);

CREATE FUNCTION talos.audit_event_immutable() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'talos.audit_event is append-only: % is not permitted', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_event_no_mutation
    BEFORE UPDATE OR DELETE ON talos.audit_event
    FOR EACH ROW EXECUTE FUNCTION talos.audit_event_immutable();
