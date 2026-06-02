// Package domain holds Talos's business aggregates. The audit event is
// immutable: it carries who did what to which resource, when, plus optional
// structured changes and metadata.
package domain

import (
	"time"

	"github.com/fromforgesoftware/go-kit/resource"
)

// ResourceTypeAuditEvent is the JSON:API type for /api/audit-events.
const ResourceTypeAuditEvent resource.Type = "audit-events"

// AuditEvent is one immutable record in the stream: who did what to which
// resource, when, with optional structured changes and metadata. Events are
// append-only — there is no mutation surface on the aggregate.
type AuditEvent interface {
	resource.Resource
	Timestamp() time.Time
	RealmID() string
	ActorID() string
	ActorType() string
	ResourceType() string
	ResourceID() string
	Action() string
	Summary() string
	Changes() map[string]any
	Metadata() map[string]any
	IP() string
	RequestID() string
}

type auditEvent struct {
	resource.Resource

	timestamp    time.Time
	realmID      string
	actorID      string
	actorType    string
	resourceType string
	resourceID   string
	action       string
	summary      string
	changes      map[string]any
	metadata     map[string]any
	ip           string
	requestID    string
}

type AuditEventOption func(*auditEvent)

func WithAuditEventID(id string) AuditEventOption {
	return func(e *auditEvent) { e.Resource = resource.Update(e.Resource, resource.WithID(id)) }
}
func WithAuditEventTimestamp(t time.Time) AuditEventOption {
	return func(e *auditEvent) { e.timestamp = t }
}
func WithAuditEventRealmID(id string) AuditEventOption {
	return func(e *auditEvent) { e.realmID = id }
}
func WithAuditEventActor(id, actorType string) AuditEventOption {
	return func(e *auditEvent) { e.actorID = id; e.actorType = actorType }
}
func WithAuditEventResource(resourceType, resourceID string) AuditEventOption {
	return func(e *auditEvent) { e.resourceType = resourceType; e.resourceID = resourceID }
}
func WithAuditEventSummary(s string) AuditEventOption {
	return func(e *auditEvent) { e.summary = s }
}
func WithAuditEventChanges(c map[string]any) AuditEventOption {
	return func(e *auditEvent) { e.changes = c }
}
func WithAuditEventMetadata(m map[string]any) AuditEventOption {
	return func(e *auditEvent) { e.metadata = m }
}
func WithAuditEventIP(ip string) AuditEventOption {
	return func(e *auditEvent) { e.ip = ip }
}
func WithAuditEventRequestID(id string) AuditEventOption {
	return func(e *auditEvent) { e.requestID = id }
}

// NewAuditEvent builds an audit-event aggregate. action is the only
// mandatory field; id and timestamp default at the storage layer when unset.
func NewAuditEvent(action string, opts ...AuditEventOption) AuditEvent {
	e := &auditEvent{
		Resource: resource.New(resource.WithType(ResourceTypeAuditEvent)),
		action:   action,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *auditEvent) Timestamp() time.Time     { return e.timestamp }
func (e *auditEvent) RealmID() string          { return e.realmID }
func (e *auditEvent) ActorID() string          { return e.actorID }
func (e *auditEvent) ActorType() string        { return e.actorType }
func (e *auditEvent) ResourceType() string     { return e.resourceType }
func (e *auditEvent) ResourceID() string       { return e.resourceID }
func (e *auditEvent) Action() string           { return e.action }
func (e *auditEvent) Summary() string          { return e.summary }
func (e *auditEvent) Changes() map[string]any  { return e.changes }
func (e *auditEvent) Metadata() map[string]any { return e.metadata }
func (e *auditEvent) IP() string               { return e.ip }
func (e *auditEvent) RequestID() string        { return e.requestID }
