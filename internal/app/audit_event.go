// Package app holds Talos's usecases and the repository ports they depend
// on. Persistence and transport adapters implement these ports.
package app

import (
	"context"
	"time"

	"github.com/fromforgesoftware/go-kit/application/repository"
	"github.com/fromforgesoftware/go-kit/application/usecase"
	apierrors "github.com/fromforgesoftware/go-kit/errors"
	"github.com/fromforgesoftware/go-kit/filter"
	"github.com/fromforgesoftware/go-kit/resource"
	"github.com/fromforgesoftware/go-kit/search"
	"github.com/fromforgesoftware/go-kit/search/query"

	"github.com/fromforgesoftware/talos/internal/domain"
	"github.com/fromforgesoftware/talos/internal/fields"
)

// defaultQueryLimit bounds an unfiltered Query so a caller can't pull the
// whole stream by accident.
const defaultQueryLimit = 100

// AuditEventFilter narrows a Query over the stream. Zero-value fields are
// not constrained; From/To bound the timestamp range inclusively.
type AuditEventFilter struct {
	ActorID      string
	ResourceType string
	ResourceID   string
	Action       string
	From         *time.Time
	To           *time.Time
	Limit        int
}

// Matches reports whether an event satisfies the filter. It's the in-memory
// twin of QueryOptions used by the live broker, so a subscriber and a query
// with the same filter select the same events. Pure — unit-tested.
func (f AuditEventFilter) Matches(e domain.AuditEvent) bool {
	if e == nil {
		return false
	}
	if f.ActorID != "" && e.ActorID() != f.ActorID {
		return false
	}
	if f.ResourceType != "" && e.ResourceType() != f.ResourceType {
		return false
	}
	if f.ResourceID != "" && e.ResourceID() != f.ResourceID {
		return false
	}
	if f.Action != "" && e.Action() != f.Action {
		return false
	}
	if f.From != nil && e.Timestamp().Before(*f.From) {
		return false
	}
	if f.To != nil && e.Timestamp().After(*f.To) {
		return false
	}
	return true
}

// AuditEventRepository persists and reads the append-only event stream. List
// backs the JSON:API REST surface (filters parsed from the request); Query
// backs the gRPC surface (a typed filter). Both read the same rows.
type AuditEventRepository interface {
	repository.Creator[domain.AuditEvent]
	repository.Getter[domain.AuditEvent]
	repository.Lister[domain.AuditEvent]
	Query(ctx context.Context, f AuditEventFilter) (resource.ListResponse[domain.AuditEvent], error)
}

// AuditEventUsecase is the ingest + read surface for the stream. Append
// validates and records one event; Query/List read with an applied filter.
type AuditEventUsecase interface {
	repository.Getter[domain.AuditEvent]
	repository.Lister[domain.AuditEvent]
	Append(ctx context.Context, event domain.AuditEvent) (domain.AuditEvent, error)
	Query(ctx context.Context, f AuditEventFilter) (resource.ListResponse[domain.AuditEvent], error)
	// Subscribe opens a filtered live feed of appended events. It errors when
	// the service runs without a broker (live tail disabled).
	Subscribe(f AuditEventFilter) (*Subscription, error)
}

type auditEventUsecase struct {
	usecase.Getter[domain.AuditEvent]
	usecase.Lister[domain.AuditEvent]

	events AuditEventRepository
	fanout *Fanout
	broker *Broker
}

// AuditEventUsecaseOption configures the usecase.
type AuditEventUsecaseOption func(*auditEventUsecase)

// WithFanout fans every appended event out to the registered forwarders.
func WithFanout(f *Fanout) AuditEventUsecaseOption {
	return func(uc *auditEventUsecase) { uc.fanout = f }
}

// WithBroker enables the live tail: Subscribe hands out feeds backed by this
// broker. The broker should also be registered in the Fanout so appended
// events reach it.
func WithBroker(b *Broker) AuditEventUsecaseOption {
	return func(uc *auditEventUsecase) { uc.broker = b }
}

func NewAuditEventUsecase(events AuditEventRepository, opts ...AuditEventUsecaseOption) AuditEventUsecase {
	uc := &auditEventUsecase{
		Getter: usecase.NewGetter(events, domain.ResourceTypeAuditEvent),
		Lister: usecase.NewLister[domain.AuditEvent](events),
		events: events,
	}
	for _, opt := range opts {
		opt(uc)
	}
	return uc
}

func (uc *auditEventUsecase) Append(ctx context.Context, event domain.AuditEvent) (domain.AuditEvent, error) {
	if event == nil || event.Action() == "" {
		return nil, apierrors.InvalidArgument("action is required")
	}
	created, err := uc.events.Create(ctx, event)
	if err != nil {
		return nil, err
	}
	if uc.fanout != nil {
		uc.fanout.Forward(ctx, created)
	}
	return created, nil
}

func (uc *auditEventUsecase) Query(ctx context.Context, f AuditEventFilter) (resource.ListResponse[domain.AuditEvent], error) {
	return uc.events.Query(ctx, f)
}

func (uc *auditEventUsecase) Subscribe(f AuditEventFilter) (*Subscription, error) {
	if uc.broker == nil {
		return nil, apierrors.New(apierrors.CodeServiceUnavailable, apierrors.WithMessage("live tail is not enabled"))
	}
	return uc.broker.Subscribe(f), nil
}

// QueryOptions translates a filter into the kit search options the repository
// applies. Newest-first ordering and a defaulted limit are part of the
// stream's read contract, so they live here rather than in the db layer.
func (f AuditEventFilter) QueryOptions() []search.Option {
	opts := []query.Option{query.SortBy(fields.Timestamp, query.SortDesc)}
	if f.ActorID != "" {
		opts = append(opts, query.FilterBy(filter.OpEq, fields.ActorID, f.ActorID))
	}
	if f.ResourceType != "" {
		opts = append(opts, query.FilterBy(filter.OpEq, fields.ResourceType, f.ResourceType))
	}
	if f.ResourceID != "" {
		opts = append(opts, query.FilterBy(filter.OpEq, fields.ResourceID, f.ResourceID))
	}
	if f.Action != "" {
		opts = append(opts, query.FilterBy(filter.OpEq, fields.Action, f.Action))
	}
	switch {
	case f.From != nil && f.To != nil:
		opts = append(opts, query.FilterBy(filter.OpBetween, fields.Timestamp, []time.Time{*f.From, *f.To}))
	case f.From != nil:
		opts = append(opts, query.FilterBy(filter.OpGTEq, fields.Timestamp, *f.From))
	case f.To != nil:
		opts = append(opts, query.FilterBy(filter.OpLTEq, fields.Timestamp, *f.To))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	opts = append(opts, query.Pagination(limit, 0))
	return []search.Option{search.WithQueryOpts(opts...)}
}
