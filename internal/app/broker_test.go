package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/internaltest"
)

func TestBroker_DeliversMatchingEvents(t *testing.T) {
	broker := app.NewBroker()
	sub := broker.Subscribe(app.AuditEventFilter{ActorID: "actor-1"})
	defer sub.Close()

	match := internaltest.NewAuditEvent(internaltest.WithAEActor("actor-1", "user"))
	other := internaltest.NewAuditEvent(internaltest.WithAEActor("actor-2", "user"))

	require.NoError(t, broker.Forward(context.Background(), other))
	require.NoError(t, broker.Forward(context.Background(), match))

	select {
	case got := <-sub.C:
		assert.Equal(t, "actor-1", got.ActorID(), "only the matching event is delivered")
	case <-time.After(time.Second):
		t.Fatal("expected an event")
	}
	select {
	case got := <-sub.C:
		t.Fatalf("did not expect a second event, got %s", got.ActorID())
	default:
	}
}

func TestBroker_CloseDetaches(t *testing.T) {
	broker := app.NewBroker()
	sub := broker.Subscribe(app.AuditEventFilter{})
	sub.Close()

	// Forwarding after Close must not panic on a closed channel.
	require.NoError(t, broker.Forward(context.Background(), internaltest.NewAuditEvent()))

	_, open := <-sub.C
	assert.False(t, open, "channel is closed after Close")
}

func TestBroker_DropsWhenSubscriberLags(t *testing.T) {
	broker := app.NewBroker()
	sub := broker.Subscribe(app.AuditEventFilter{})
	defer sub.Close()

	// Overfill the buffer without draining; excess events are dropped, never
	// blocking the forwarder.
	for i := 0; i < 1000; i++ {
		require.NoError(t, broker.Forward(context.Background(), internaltest.NewAuditEvent()))
	}
}

func TestAuditEventFilter_Matches(t *testing.T) {
	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	event := internaltest.NewAuditEvent(
		internaltest.WithAEActor("actor-1", "user"),
		internaltest.WithAEResource("doc", "doc-1"),
		internaltest.WithAEAction("doc.updated"),
		internaltest.WithAETimestamp(time.Now()),
	)

	assert.True(t, app.AuditEventFilter{}.Matches(event), "empty filter matches all")
	assert.True(t, app.AuditEventFilter{ActorID: "actor-1", Action: "doc.updated"}.Matches(event))
	assert.True(t, (app.AuditEventFilter{From: &from, To: &to}).Matches(event))
	assert.False(t, app.AuditEventFilter{ActorID: "actor-2"}.Matches(event))
	assert.False(t, app.AuditEventFilter{ResourceType: "user"}.Matches(event))
	assert.False(t, (app.AuditEventFilter{From: &to}).Matches(event), "event before From is excluded")
}
