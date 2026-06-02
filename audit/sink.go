// Package audit adapts the kit audit Sink port to Talos's client, so a
// deployment can forward another service's audit events to Talos without
// that service importing Talos.
package audit

import (
	"context"

	kitaudit "github.com/fromforgesoftware/go-kit/audit"
	talos "github.com/fromforgesoftware/talos/pkg/client"
)

// Emitter is the slice of the Talos client the sink needs.
type Emitter interface {
	Append(ctx context.Context, e talos.Event) (string, error)
}

// Sink forwards kit audit events to Talos over its client.
type Sink struct {
	client Emitter
}

func NewSink(client Emitter) *Sink {
	return &Sink{client: client}
}

func (s *Sink) Emit(ctx context.Context, e kitaudit.Event) error {
	_, err := s.client.Append(ctx, talos.Event{
		ID:           e.ID,
		Timestamp:    e.Timestamp,
		RealmID:      e.RealmID,
		ActorID:      e.ActorID,
		ActorType:    e.ActorType,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		Action:       e.Action,
		Summary:      e.Summary,
		Changes:      e.Changes,
		Metadata:     e.Metadata,
		IP:           e.IP,
		RequestID:    e.RequestID,
	})
	return err
}
