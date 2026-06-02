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

CREATE TABLE talos.audit_event_2026_05 PARTITION OF talos.audit_event
    FOR VALUES FROM ('2026-05-01 00:00:00+00') TO ('2026-06-01 00:00:00+00');
CREATE TABLE talos.audit_event_2026_06 PARTITION OF talos.audit_event
    FOR VALUES FROM ('2026-06-01 00:00:00+00') TO ('2026-07-01 00:00:00+00');
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
