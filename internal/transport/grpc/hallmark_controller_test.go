package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/fromforgesoftware/go-kit/resource"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/app/apptest"
	"github.com/fromforgesoftware/talos/internal/domain"
	"github.com/fromforgesoftware/talos/internal/internaltest"
	talosgrpc "github.com/fromforgesoftware/talos/internal/transport/grpc"
	talosv1 "github.com/fromforgesoftware/talos/pkg/api/talos/v1"
)

// fakeSubscribeStream captures Subscribe sends and lets a test drive the
// stream's context lifetime.
type fakeSubscribeStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent chan *talosv1.SubscribeResponse
}

func newFakeSubscribeStream(ctx context.Context) *fakeSubscribeStream {
	return &fakeSubscribeStream{ctx: ctx, sent: make(chan *talosv1.SubscribeResponse, 64)}
}

func (s *fakeSubscribeStream) Context() context.Context     { return s.ctx }
func (s *fakeSubscribeStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeSubscribeStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeSubscribeStream) SetTrailer(metadata.MD)       {}
func (s *fakeSubscribeStream) Send(resp *talosv1.SubscribeResponse) error {
	s.sent <- resp
	return nil
}

func TestSubscribe_ReplaysHistoryThenLive(t *testing.T) {
	uc := apptest.NewAuditEventUsecase(t)
	broker := app.NewBroker()

	// Replay returns two history events (newest-first, as Query contracts).
	hist := []domain.AuditEvent{
		internaltest.NewAuditEvent(internaltest.WithAEID("evt-2"), internaltest.WithAEAction("a2")),
		internaltest.NewAuditEvent(internaltest.WithAEID("evt-1"), internaltest.WithAEAction("a1")),
	}
	uc.EXPECT().Query(mock.Anything, mock.MatchedBy(func(f app.AuditEventFilter) bool {
		return f.From != nil
	})).Return(resource.NewListResponse(hist, 2), nil)
	uc.EXPECT().Subscribe(mock.Anything).Return(broker.Subscribe(app.AuditEventFilter{}), nil)

	ctx, cancel := context.WithCancel(context.Background())
	stream := newFakeSubscribeStream(ctx)
	c := talosgrpc.NewTalosController(uc)

	server := c.(talosv1.TalosServiceServer)
	done := make(chan error, 1)
	go func() {
		done <- server.Subscribe(&talosv1.SubscribeRequest{ReplayFrom: timestamppb.Now()}, stream)
	}()

	// Replay arrives oldest-first.
	assert.Equal(t, "evt-1", recvWithin(t, stream).GetEvent().GetId())
	assert.Equal(t, "evt-2", recvWithin(t, stream).GetEvent().GetId())

	// A live event after replay is forwarded; one already replayed is skipped.
	require.NoError(t, broker.Forward(ctx, internaltest.NewAuditEvent(internaltest.WithAEID("evt-2"), internaltest.WithAEAction("a2"))))
	require.NoError(t, broker.Forward(ctx, internaltest.NewAuditEvent(internaltest.WithAEID("evt-3"), internaltest.WithAEAction("a3"))))
	assert.Equal(t, "evt-3", recvWithin(t, stream).GetEvent().GetId(), "replayed event is deduped, only the new one streams")

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancel")
	}
}

func recvWithin(t *testing.T, s *fakeSubscribeStream) *talosv1.SubscribeResponse {
	t.Helper()
	select {
	case resp := <-s.sent:
		return resp
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a streamed event")
		return nil
	}
}
