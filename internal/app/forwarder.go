package app

import (
	"context"

	"github.com/fromforgesoftware/go-kit/monitoring/logger"

	"github.com/fromforgesoftware/talos/internal/domain"
)

// Forwarder fans a stored audit event out to an external sink (Kafka, NATS,
// S3, a SIEM…). Built-in: LogForwarder. Others satisfy the same port.
type Forwarder interface {
	Name() string
	Forward(ctx context.Context, event domain.AuditEvent) error
}

// Fanout delivers each event to every registered forwarder, best-effort: a
// forwarder failure is logged and never blocks ingest or the others. (Async
// queueing is a later refinement.)
type Fanout struct {
	forwarders []Forwarder
	log        logger.Logger
}

func NewFanout(forwarders ...Forwarder) *Fanout {
	return &Fanout{forwarders: forwarders, log: logger.New()}
}

func (f *Fanout) Forward(ctx context.Context, event domain.AuditEvent) {
	for _, fw := range f.forwarders {
		if err := fw.Forward(ctx, event); err != nil {
			f.log.ErrorContext(ctx, "audit forward failed", "forwarder", fw.Name(), "error", err)
		}
	}
}

// LogForwarder is the zero-config forwarder: it writes events to the log. A
// useful default and a template for real sinks.
type LogForwarder struct {
	log logger.Logger
}

func NewLogForwarder() *LogForwarder {
	return &LogForwarder{log: logger.New()}
}

func (LogForwarder) Name() string { return "log" }

func (f *LogForwarder) Forward(ctx context.Context, e domain.AuditEvent) error {
	f.log.InfoContext(ctx, "audit",
		"action", e.Action(),
		"actorId", e.ActorID(),
		"resourceType", e.ResourceType(),
		"resourceId", e.ResourceID(),
	)
	return nil
}
