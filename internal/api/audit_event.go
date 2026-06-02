// Package api holds Talos's JSON:API DTOs and the mappers between them and
// the domain aggregates.
package api

import (
	"time"

	"github.com/fromforgesoftware/go-kit/resource"

	"github.com/fromforgesoftware/talos/internal/domain"
)

// ResourceTypeAuditEvent is the JSON:API type for /api/audit-events.
const ResourceTypeAuditEvent resource.Type = "audit-events"

// AuditEventDTO is the read-only wire shape for an audit event. The REST
// surface never decodes it for writes — events are appended over gRPC.
type AuditEventDTO struct {
	resource.RestDTO

	RTimestamp    time.Time      `jsonapi:"attr,timestamp,omitempty"`
	RRealmID      string         `jsonapi:"attr,realmId,omitempty"`
	RActorID      string         `jsonapi:"attr,actorId,omitempty"`
	RActorType    string         `jsonapi:"attr,actorType,omitempty"`
	RResourceType string         `jsonapi:"attr,resourceType,omitempty"`
	RResourceID   string         `jsonapi:"attr,resourceId,omitempty"`
	RAction       string         `jsonapi:"attr,action,omitempty"`
	RSummary      string         `jsonapi:"attr,summary,omitempty"`
	RChanges      map[string]any `jsonapi:"attr,changes,omitempty"`
	RMetadata     map[string]any `jsonapi:"attr,metadata,omitempty"`
	RIP           string         `jsonapi:"attr,ip,omitempty"`
	RRequestID    string         `jsonapi:"attr,requestId,omitempty"`
}

func AuditEventToDTO(e domain.AuditEvent) *AuditEventDTO {
	if e == nil {
		return nil
	}
	dto := &AuditEventDTO{
		RestDTO:       resource.ToRestDTO(e),
		RTimestamp:    e.Timestamp(),
		RRealmID:      e.RealmID(),
		RActorID:      e.ActorID(),
		RActorType:    e.ActorType(),
		RResourceType: e.ResourceType(),
		RResourceID:   e.ResourceID(),
		RAction:       e.Action(),
		RSummary:      e.Summary(),
		RChanges:      e.Changes(),
		RMetadata:     e.Metadata(),
		RIP:           e.IP(),
		RRequestID:    e.RequestID(),
	}
	dto.RType = ResourceTypeAuditEvent
	return dto
}
