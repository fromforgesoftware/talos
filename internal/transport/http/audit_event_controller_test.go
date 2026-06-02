package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fromforgesoftware/go-kit/auth/jwt"
	gws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fromforgesoftware/go-kit/resource"
	kitws "github.com/fromforgesoftware/go-kit/transport/websocket"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/domain"
	"github.com/fromforgesoftware/talos/internal/internaltest"
)

func TestAuditPayload(t *testing.T) {
	e := internaltest.NewAuditEvent(
		internaltest.WithAEID("evt-1"),
		internaltest.WithAEActor("actor-1", "user"),
		internaltest.WithAEAction("doc.updated"),
	)
	payload := auditPayload(e)
	assert.Equal(t, "evt-1", payload["id"])
	assert.Equal(t, "actor-1", payload["actorId"])
	assert.Equal(t, "doc.updated", payload["action"])
}

func TestParseOrigins(t *testing.T) {
	assert.Nil(t, parseOrigins(""))
	assert.Equal(t, []string{"https://a.io", "https://b.io"},
		parseOrigins(" https://a.io , https://b.io ,"))
}

func TestOriginAllowed(t *testing.T) {
	// Empty allow-list permits any origin (dev default).
	assert.True(t, originAllowed(nil, "https://anything.io"))

	allowed := []string{"https://console.example.com"}
	assert.True(t, originAllowed(allowed, "https://console.example.com"))
	assert.False(t, originAllowed(allowed, "https://evil.example.com"))
	assert.False(t, originAllowed(allowed, ""))
}

func TestFilterFromData(t *testing.T) {
	f := filterFromData(map[string]any{"action": "doc.updated", "resourceType": "doc", "ignored": 7})
	assert.Equal(t, "doc.updated", f.Action)
	assert.Equal(t, "doc", f.ResourceType)
	assert.Empty(t, f.ActorID)

	// A nil/!map data is tolerated as an empty (match-all) filter.
	assert.Equal(t, app.AuditEventFilter{}, filterFromData(nil))
}

func dialStream(t *testing.T, c *AuditEventController) (*gws.Conn, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(c.stream))
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := gws.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	return conn, func() { _ = conn.Close(); srv.Close() }
}

// readUntil returns the first frame of the given type, skipping the welcome and
// any others, so a test can assert on the frame it cares about.
func readUntil(t *testing.T, conn *gws.Conn, want kitws.MessageType) kitws.Message {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	for {
		var msg kitws.Message
		require.NoError(t, conn.ReadJSON(&msg))
		if msg.Type == want {
			return msg
		}
	}
}

func newStreamController(t *testing.T) (*AuditEventController, *app.Broker) {
	t.Helper()
	broker := app.NewBroker()
	uc := app.NewAuditEventUsecase(stubAuditRepo{}, app.WithFanout(app.NewFanout(broker)), app.WithBroker(broker))
	return &AuditEventController{events: uc}, broker
}

func TestStream_SubscribeThenReceiveTaggedFrame(t *testing.T) {
	c, broker := newStreamController(t)
	conn, cleanup := dialStream(t, c)
	defer cleanup()

	require.NoError(t, conn.WriteJSON(kitws.Message{
		Type: kitws.MessageTypeSubscribe, ID: "panel-1",
		Data: map[string]any{"action": "doc.updated"},
	}))
	ack := readUntil(t, conn, kitws.MessageTypeAck)
	assert.Equal(t, "panel-1", ack.ID)

	require.NoError(t, broker.Forward(context.Background(),
		internaltest.NewAuditEvent(internaltest.WithAEID("evt-1"), internaltest.WithAEAction("doc.updated"))))

	frame := readUntil(t, conn, kitws.MessageTypeMessage)
	assert.Equal(t, "panel-1", frame.Subject, "frame is tagged with the subscription id")
	assert.Equal(t, int64(1), frame.SequenceNumber)
	data, _ := frame.Data.(map[string]any)
	assert.Equal(t, "evt-1", data["id"])
}

func TestStream_TwoSubscriptionsMultiplexedAndFiltered(t *testing.T) {
	c, broker := newStreamController(t)
	conn, cleanup := dialStream(t, c)
	defer cleanup()

	require.NoError(t, conn.WriteJSON(kitws.Message{Type: kitws.MessageTypeSubscribe, ID: "a", Data: map[string]any{"action": "doc.created"}}))
	require.NoError(t, conn.WriteJSON(kitws.Message{Type: kitws.MessageTypeSubscribe, ID: "b", Data: map[string]any{"action": "doc.deleted"}}))
	readUntil(t, conn, kitws.MessageTypeAck)
	readUntil(t, conn, kitws.MessageTypeAck)

	// Only subscription "b" matches; "a"'s filter excludes it.
	require.NoError(t, broker.Forward(context.Background(),
		internaltest.NewAuditEvent(internaltest.WithAEID("evt-del"), internaltest.WithAEAction("doc.deleted"))))

	frame := readUntil(t, conn, kitws.MessageTypeMessage)
	assert.Equal(t, "b", frame.Subject, "the deleted event routes only to subscription b")
}

func TestStream_Unsubscribe(t *testing.T) {
	c, broker := newStreamController(t)
	conn, cleanup := dialStream(t, c)
	defer cleanup()

	require.NoError(t, conn.WriteJSON(kitws.Message{Type: kitws.MessageTypeSubscribe, ID: "x", Data: map[string]any{}}))
	readUntil(t, conn, kitws.MessageTypeAck)

	require.NoError(t, conn.WriteJSON(kitws.Message{Type: messageTypeUnsubscribe, ID: "x"}))
	ack := readUntil(t, conn, kitws.MessageTypeAck)
	assert.True(t, ack.Response, "unsubscribe is acked with response=true")

	_ = broker // after unsubscribe the session holds no subscriptions
}

func TestStream_ReplayThenLiveDedups(t *testing.T) {
	broker := app.NewBroker()
	// Query returns history newest-first (the repo contract); replay reverses it.
	history := []domain.AuditEvent{
		internaltest.NewAuditEvent(internaltest.WithAEID("evt-2"), internaltest.WithAEAction("a2")),
		internaltest.NewAuditEvent(internaltest.WithAEID("evt-1"), internaltest.WithAEAction("a1")),
	}
	uc := app.NewAuditEventUsecase(replayRepo{history: history},
		app.WithFanout(app.NewFanout(broker)), app.WithBroker(broker))
	c := &AuditEventController{events: uc}

	conn, cleanup := dialStream(t, c)
	defer cleanup()

	require.NoError(t, conn.WriteJSON(kitws.Message{
		Type: kitws.MessageTypeSubscribe, ID: "p",
		Data: map[string]any{"replayFrom": "2026-01-01T00:00:00Z"},
	}))
	readUntil(t, conn, kitws.MessageTypeAck)

	// Replay arrives oldest-first with ascending sequence numbers.
	f1 := readUntil(t, conn, kitws.MessageTypeMessage)
	assert.Equal(t, "evt-1", f1.Data.(map[string]any)["id"])
	assert.Equal(t, int64(1), f1.SequenceNumber)
	f2 := readUntil(t, conn, kitws.MessageTypeMessage)
	assert.Equal(t, "evt-2", f2.Data.(map[string]any)["id"])
	assert.Equal(t, int64(2), f2.SequenceNumber)

	// A replayed id arriving live is deduped; a fresh one streams as sn 3.
	require.NoError(t, broker.Forward(context.Background(),
		internaltest.NewAuditEvent(internaltest.WithAEID("evt-2"), internaltest.WithAEAction("a2"))))
	require.NoError(t, broker.Forward(context.Background(),
		internaltest.NewAuditEvent(internaltest.WithAEID("evt-3"), internaltest.WithAEAction("a3"))))

	live := readUntil(t, conn, kitws.MessageTypeMessage)
	assert.Equal(t, "evt-3", live.Data.(map[string]any)["id"], "replayed evt-2 is skipped; only evt-3 streams")
	assert.Equal(t, int64(3), live.SequenceNumber)
}

func TestStream_TokenAuth(t *testing.T) {
	broker := app.NewBroker()
	uc := app.NewAuditEventUsecase(stubAuditRepo{}, app.WithFanout(app.NewFanout(broker)), app.WithBroker(broker))
	c := &AuditEventController{events: uc, validator: stubValidator{}}

	srv := httptest.NewServer(http.HandlerFunc(c.stream))
	defer srv.Close()

	// No token → 401 (returned before the WS upgrade, so a plain GET sees it).
	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Bad token → 401.
	resp, err = http.Get(srv.URL + "?access_token=bad")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// Good token → the upgrade succeeds.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?access_token=good"
	conn, _, err := gws.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
}

// stubAuditRepo satisfies AuditEventRepository; the live-only stream path never
// reads, so the methods are unused.
type stubAuditRepo struct{ app.AuditEventRepository }

// stubValidator accepts the token "good" and rejects everything else.
type stubValidator struct{}

func (stubValidator) Validate(_ context.Context, token string) (*jwt.Claims, error) {
	if token == "good" {
		return &jwt.Claims{}, nil
	}
	return nil, errInvalidToken
}

var errInvalidToken = fmt.Errorf("invalid token")

// replayRepo serves a fixed history from Query so the replay path can be tested
// without a database.
type replayRepo struct {
	app.AuditEventRepository
	history []domain.AuditEvent
}

func (r replayRepo) Query(context.Context, app.AuditEventFilter) (resource.ListResponse[domain.AuditEvent], error) {
	return resource.NewListResponse(r.history, len(r.history)), nil
}
