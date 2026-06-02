// Package internaltest holds test builders + matchers shared by Talos's
// unit and integration tests.
package internaltest

import (
	"time"

	"github.com/fromforgesoftware/talos/internal/domain"
)

type AuditEventOption func(*auditEventOpts)

type auditEventOpts struct {
	id           string
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

func defaultAuditEventOptions() []AuditEventOption {
	return []AuditEventOption{
		WithAEAction("resource.created"),
		WithAEActor("actor-test", "user"),
		WithAEResource("doc", "doc-1"),
	}
}

func WithAEID(id string) AuditEventOption { return func(o *auditEventOpts) { o.id = id } }
func WithAETimestamp(t time.Time) AuditEventOption {
	return func(o *auditEventOpts) { o.timestamp = t }
}
func WithAERealmID(id string) AuditEventOption   { return func(o *auditEventOpts) { o.realmID = id } }
func WithAEAction(a string) AuditEventOption     { return func(o *auditEventOpts) { o.action = a } }
func WithAESummary(s string) AuditEventOption    { return func(o *auditEventOpts) { o.summary = s } }
func WithAEIP(ip string) AuditEventOption        { return func(o *auditEventOpts) { o.ip = ip } }
func WithAERequestID(id string) AuditEventOption { return func(o *auditEventOpts) { o.requestID = id } }
func WithAEActor(id, actorType string) AuditEventOption {
	return func(o *auditEventOpts) { o.actorID = id; o.actorType = actorType }
}
func WithAEResource(resourceType, resourceID string) AuditEventOption {
	return func(o *auditEventOpts) { o.resourceType = resourceType; o.resourceID = resourceID }
}
func WithAEChanges(c map[string]any) AuditEventOption {
	return func(o *auditEventOpts) { o.changes = c }
}
func WithAEMetadata(m map[string]any) AuditEventOption {
	return func(o *auditEventOpts) { o.metadata = m }
}

func NewAuditEvent(opts ...AuditEventOption) domain.AuditEvent {
	o := &auditEventOpts{}
	for _, opt := range append(defaultAuditEventOptions(), opts...) {
		opt(o)
	}
	domainOpts := []domain.AuditEventOption{
		domain.WithAuditEventRealmID(o.realmID),
		domain.WithAuditEventActor(o.actorID, o.actorType),
		domain.WithAuditEventResource(o.resourceType, o.resourceID),
		domain.WithAuditEventSummary(o.summary),
		domain.WithAuditEventChanges(o.changes),
		domain.WithAuditEventMetadata(o.metadata),
		domain.WithAuditEventIP(o.ip),
		domain.WithAuditEventRequestID(o.requestID),
	}
	if o.id != "" {
		domainOpts = append(domainOpts, domain.WithAuditEventID(o.id))
	}
	if !o.timestamp.IsZero() {
		domainOpts = append(domainOpts, domain.WithAuditEventTimestamp(o.timestamp))
	}
	return domain.NewAuditEvent(o.action, domainOpts...)
}

// MatchAuditEvent compares action + actor + resource + summary, ignoring
// id/timestamp so it works against pre-persist aggregates.
func MatchAuditEvent(want domain.AuditEvent) func(domain.AuditEvent) bool {
	return func(got domain.AuditEvent) bool {
		if want == nil {
			return got == nil
		}
		if got == nil {
			return false
		}
		return want.Action() == got.Action() &&
			want.ActorID() == got.ActorID() &&
			want.ActorType() == got.ActorType() &&
			want.ResourceType() == got.ResourceType() &&
			want.ResourceID() == got.ResourceID() &&
			want.Summary() == got.Summary()
	}
}
