package app

import (
	"context"
	"sync"

	"github.com/fromforgesoftware/go-kit/monitoring/logger"

	"github.com/fromforgesoftware/talos/internal/domain"
)

// subscriberBuffer bounds each subscriber's in-flight backlog. A subscriber
// that can't keep up has events dropped (logged) rather than blocking ingest —
// the audit stream's durable copy is the database, the live tail is best-effort.
const subscriberBuffer = 256

// Broker is the in-process fan-out hub behind the live tail. It implements
// Forwarder, so registering it in the Fanout makes every appended event
// available to subscribers; Subscribe hands a caller a filtered channel.
type Broker struct {
	mu     sync.RWMutex
	subs   map[int]*subscriber
	nextID int
	log    logger.Logger
}

type subscriber struct {
	filter AuditEventFilter
	ch     chan domain.AuditEvent
}

// Subscription is a live feed of matching events. Close detaches it and
// releases the channel; callers must Close when done (e.g. on stream end).
type Subscription struct {
	C      <-chan domain.AuditEvent
	broker *Broker
	id     int
}

func NewBroker() *Broker {
	return &Broker{subs: map[int]*subscriber{}, log: logger.New()}
}

func (b *Broker) Name() string { return "broker" }

// Forward publishes an event to every matching subscriber, non-blocking: a
// full subscriber channel drops the event so one slow consumer never stalls
// ingest or the other subscribers.
func (b *Broker) Forward(ctx context.Context, event domain.AuditEvent) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subs {
		if !sub.filter.Matches(event) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			b.log.WarnContext(ctx, "audit subscriber lagging; dropping event",
				"action", event.Action(), "resourceId", event.ResourceID())
		}
	}
	return nil
}

// Subscribe registers a filtered live feed and returns its Subscription.
func (b *Broker) Subscribe(filter AuditEventFilter) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	ch := make(chan domain.AuditEvent, subscriberBuffer)
	b.subs[id] = &subscriber{filter: filter, ch: ch}
	return &Subscription{C: ch, broker: b, id: id}
}

// Close detaches the subscription. It's safe to call more than once.
func (s *Subscription) Close() {
	if s.broker == nil {
		return
	}
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	if sub, ok := s.broker.subs[s.id]; ok {
		delete(s.broker.subs, s.id)
		close(sub.ch)
	}
	s.broker = nil
}
