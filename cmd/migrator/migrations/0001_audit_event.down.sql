DROP TRIGGER IF EXISTS audit_event_no_mutation ON talos.audit_event;
DROP FUNCTION IF EXISTS talos.audit_event_immutable();
DROP TABLE IF EXISTS talos.audit_event;
