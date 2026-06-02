package http

import (
	"context"
	"strings"
	"sync"
	"time"

	gws "github.com/gorilla/websocket"

	"github.com/fromforgesoftware/go-kit/monitoring/logger"
	kitws "github.com/fromforgesoftware/go-kit/transport/websocket"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/domain"
)

const (
	// wsWriteWait bounds a single frame write; wsPingInterval keeps the
	// connection alive (and detects dead peers via the failing write).
	wsWriteWait    = 10 * time.Second
	wsPingInterval = 30 * time.Second
	// outboundBuffer caps a session's pending server→client frames; a client
	// that can't keep up has frames dropped rather than stalling the others.
	outboundBuffer = 256

	// streamTopicAudit tags audit frames; messageTypeUnsubscribe extends the
	// kit's message types with the unsubscribe verb.
	streamTopicAudit       kitws.TopicType   = "audit"
	messageTypeUnsubscribe kitws.MessageType = "unsubscribe"
)

// streamSession multiplexes any number of filtered audit subscriptions over a
// single WebSocket using the kit's Message envelope. The client opens a
// subscription with a `subscribe` message (its id naming the subscription) and
// closes it with `unsubscribe`; the server pushes `message` frames tagged with
// that id and a per-subscription sequence number, so the client can route
// frames to the right panel and detect gaps.
type streamSession struct {
	conn   *gws.Conn
	events app.AuditEventUsecase
	out    chan kitws.Message
	log    logger.Logger

	mu   sync.Mutex
	subs map[string]*app.Subscription
}

func newStreamSession(conn *gws.Conn, events app.AuditEventUsecase) *streamSession {
	return &streamSession{
		conn:   conn,
		events: events,
		out:    make(chan kitws.Message, outboundBuffer),
		log:    logger.New(),
		subs:   map[string]*app.Subscription{},
	}
}

// run owns the connection for its lifetime: a single write pump drains the
// outbound queue (the sole writer, as gorilla requires), while this goroutine
// reads control messages until the client disconnects.
func (s *streamSession) run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	defer func() { _ = s.conn.Close() }()
	defer s.closeAll()
	defer cancel()

	go s.writePump(ctx)
	s.enqueue(kitws.Message{Type: kitws.MessageTypeWelcome})
	s.readLoop(ctx)
}

func (s *streamSession) readLoop(ctx context.Context) {
	for {
		var msg kitws.Message
		if err := s.conn.ReadJSON(&msg); err != nil {
			return // client closed or sent a malformed frame
		}
		switch msg.Type {
		case kitws.MessageTypeSubscribe:
			s.handleSubscribe(ctx, msg)
		case messageTypeUnsubscribe:
			s.handleUnsubscribe(msg.ID)
		case kitws.MessageTypePing:
			s.enqueue(kitws.Message{Type: kitws.MessageTypePong, ID: msg.ID})
		}
	}
}

func (s *streamSession) handleSubscribe(ctx context.Context, msg kitws.Message) {
	if msg.ID == "" {
		s.enqueue(errorMessage("", "subscribe requires an id"))
		return
	}
	s.mu.Lock()
	if _, exists := s.subs[msg.ID]; exists {
		s.mu.Unlock()
		s.enqueue(errorMessage(msg.ID, "already subscribed"))
		return
	}
	filter := filterFromData(msg.Data)
	// Attach the live feed before replaying history, so events appended during
	// replay are buffered (not lost) and deduped against the replay set.
	sub, err := s.events.Subscribe(filter)
	if err != nil {
		s.mu.Unlock()
		s.enqueue(errorMessage(msg.ID, err.Error()))
		return
	}
	s.subs[msg.ID] = sub
	s.mu.Unlock()

	s.enqueue(kitws.Message{Type: kitws.MessageTypeAck, ID: msg.ID, Topic: streamTopicAudit})
	replayed, lastSN := s.replayHistory(ctx, msg.ID, filter, msg.Data)
	go s.forward(ctx, msg.ID, sub, lastSN, replayed)
}

// replayHistory, when the subscribe carried replayFrom, queries matching
// history and streams it oldest-first before the live tail, returning the
// replayed ids (so live dedups the boundary) and the last sequence number used.
func (s *streamSession) replayHistory(ctx context.Context, subID string, filter app.AuditEventFilter, data any) (map[string]struct{}, int64) {
	from, limit := replayParamsFromData(data)
	if from == nil {
		return nil, 0
	}
	filter.From = from
	filter.Limit = limit
	list, err := s.events.Query(ctx, filter)
	if err != nil {
		s.log.WarnContext(ctx, "audit replay query failed", "error", err)
		return nil, 0
	}
	history := list.Results()
	replayed := make(map[string]struct{}, len(history))
	var sn int64
	for i := len(history) - 1; i >= 0; i-- { // Query is newest-first; replay oldest-first.
		sn++
		s.enqueue(kitws.Message{
			Type:           kitws.MessageTypeMessage,
			Topic:          streamTopicAudit,
			Subject:        subID,
			SequenceNumber: sn,
			Data:           auditPayload(history[i]),
		})
		replayed[history[i].ID()] = struct{}{}
	}
	return replayed, sn
}

func (s *streamSession) handleUnsubscribe(subID string) {
	s.mu.Lock()
	sub, ok := s.subs[subID]
	if ok {
		delete(s.subs, subID)
	}
	s.mu.Unlock()
	if ok {
		sub.Close() // closes sub.C, ending the forwarder
	}
	s.enqueue(kitws.Message{Type: kitws.MessageTypeAck, ID: subID, Response: true})
}

// forward pumps one subscription's live events to the outbound queue, stamping
// each with the subscription id and a monotonic sequence number continuing from
// startSN. Events already sent during replay are skipped so the boundary never
// duplicates.
func (s *streamSession) forward(ctx context.Context, subID string, sub *app.Subscription, startSN int64, replayed map[string]struct{}) {
	sn := startSN
	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-sub.C:
			if !open {
				return
			}
			if _, seen := replayed[event.ID()]; seen {
				continue
			}
			sn++
			s.enqueue(kitws.Message{
				Type:           kitws.MessageTypeMessage,
				Topic:          streamTopicAudit,
				Subject:        subID,
				SequenceNumber: sn,
				Data:           auditPayload(event),
			})
		}
	}
}

func (s *streamSession) writePump(ctx context.Context) {
	ping := time.NewTicker(wsPingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.out:
			_ = s.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := s.conn.WriteJSON(msg); err != nil {
				return
			}
		case <-ping.C:
			_ = s.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := s.conn.WriteMessage(gws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *streamSession) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sub := range s.subs {
		sub.Close()
		delete(s.subs, id)
	}
}

// enqueue offers a frame to the write pump without blocking: a full queue drops
// the frame (logged) so one slow client never stalls the read loop or the
// forwarders.
func (s *streamSession) enqueue(msg kitws.Message) {
	select {
	case s.out <- msg:
	default:
		s.log.Warn("audit stream client lagging; dropping frame")
	}
}

func errorMessage(id, reason string) kitws.Message {
	return kitws.Message{Type: kitws.MessageTypeError, ID: id, Data: map[string]any{"error": reason}}
}

// filterFromData reads an AuditEventFilter out of a subscribe message's data
// object. Unknown/missing fields are left unconstrained.
func filterFromData(data any) app.AuditEventFilter {
	m, _ := data.(map[string]any)
	get := func(k string) string {
		v, _ := m[k].(string)
		return v
	}
	return app.AuditEventFilter{
		ActorID:      get("actorId"),
		ResourceType: get("resourceType"),
		ResourceID:   get("resourceId"),
		Action:       get("action"),
	}
}

// replayParamsFromData reads an optional replayFrom (RFC3339) + replayLimit from
// a subscribe message's data. A nil time means "live only, no replay".
func replayParamsFromData(data any) (*time.Time, int) {
	m, ok := data.(map[string]any)
	if !ok {
		return nil, 0
	}
	raw, ok := m["replayFrom"].(string)
	if !ok || raw == "" {
		return nil, 0
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, 0
	}
	limit := 0
	if n, ok := m["replayLimit"].(float64); ok {
		limit = int(n)
	}
	return &t, limit
}

// parseOrigins splits a comma-separated allow-list, trimming blanks. An empty
// input yields nil — origins are then denied unless the insecure posture is
// explicitly enabled (see originAllowed).
func parseOrigins(raw string) []string {
	var out []string
	for _, o := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// originAllowed reports whether a request Origin may open the live tail.
// Secure by default: an empty allow-list denies every origin, unless
// allowInsecure is set (the explicit TALOS_WS_ALLOW_INSECURE=1 dev opt-out),
// which permits any origin. With a non-empty allow-list the origin must match
// an entry exactly regardless of allowInsecure. Pure — unit-tested.
func originAllowed(allowed []string, allowInsecure bool, origin string) bool {
	if len(allowed) == 0 {
		return allowInsecure
	}
	for _, a := range allowed {
		if origin == a {
			return true
		}
	}
	return false
}

// auditPayload is the compact JSON shape carried in a frame's data. Pure —
// unit-tested so the wire contract is pinned.
func auditPayload(e domain.AuditEvent) map[string]any {
	return map[string]any{
		"id":           e.ID(),
		"timestamp":    e.Timestamp().Format(time.RFC3339Nano),
		"realmId":      e.RealmID(),
		"actorId":      e.ActorID(),
		"actorType":    e.ActorType(),
		"resourceType": e.ResourceType(),
		"resourceId":   e.ResourceID(),
		"action":       e.Action(),
		"summary":      e.Summary(),
	}
}
