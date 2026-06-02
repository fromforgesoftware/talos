// Package client is the consumer-facing SDK for Talos's gRPC surface.
// Other forge services dial Talos once and emit audit events through it;
// returned gRPC status codes are mapped to kit apierrors.
package client

import (
	"context"
	"errors"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	apierrors "github.com/fromforgesoftware/go-kit/errors"
	talosv1 "github.com/fromforgesoftware/talos/pkg/api/talos/v1"
)

// Event is the SDK-facing shape of an audit event to emit. ID and Timestamp
// are server-assigned when left zero.
type Event struct {
	ID           string
	Timestamp    time.Time
	RealmID      string
	ActorID      string
	ActorType    string
	ResourceType string
	ResourceID   string
	Action       string
	Summary      string
	Changes      map[string]any
	Metadata     map[string]any
	IP           string
	RequestID    string
}

// Filter narrows a Query. Zero-value fields are not constrained.
type Filter struct {
	ActorID      string
	ResourceType string
	ResourceID   string
	Action       string
	From         *time.Time
	To           *time.Time
	Limit        int
}

// Client wraps Talos's gRPC surface with kit error mapping.
type Client struct {
	talos talosv1.TalosServiceClient
}

// New builds a client over an established connection to Talos.
func New(conn grpc.ClientConnInterface) *Client {
	return &Client{talos: talosv1.NewTalosServiceClient(conn)}
}

// NewFromServiceClient is the seam tests use to inject a fake gRPC client.
func NewFromServiceClient(c talosv1.TalosServiceClient) *Client {
	return &Client{talos: c}
}

// Append records a single event and returns its server-assigned id.
func (c *Client) Append(ctx context.Context, e Event) (string, error) {
	resp, err := c.talos.Append(ctx, &talosv1.AppendRequest{Event: eventToProto(e)})
	if err != nil {
		return "", apierrors.FromGRPCError(err)
	}
	return resp.GetId(), nil
}

// AppendBatch streams a batch of events and returns how many were persisted.
func (c *Client) AppendBatch(ctx context.Context, events []Event) (int, error) {
	stream, err := c.talos.AppendStream(ctx)
	if err != nil {
		return 0, apierrors.FromGRPCError(err)
	}
	for _, e := range events {
		if err := stream.Send(&talosv1.AppendStreamRequest{Event: eventToProto(e)}); err != nil {
			return 0, apierrors.FromGRPCError(err)
		}
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return 0, apierrors.FromGRPCError(err)
	}
	return int(resp.GetCount()), nil
}

// Query returns events matching the filter, newest first.
func (c *Client) Query(ctx context.Context, f Filter) ([]Event, error) {
	req := &talosv1.QueryRequest{
		ActorId:      f.ActorID,
		ResourceType: f.ResourceType,
		ResourceId:   f.ResourceID,
		Action:       f.Action,
		Limit:        uint32(f.Limit),
	}
	if f.From != nil {
		req.From = timestamppb.New(*f.From)
	}
	if f.To != nil {
		req.To = timestamppb.New(*f.To)
	}
	resp, err := c.talos.Query(ctx, req)
	if err != nil {
		return nil, apierrors.FromGRPCError(err)
	}
	out := make([]Event, 0, len(resp.GetEvents()))
	for _, e := range resp.GetEvents() {
		out = append(out, eventFromProto(e))
	}
	return out, nil
}

// Subscribe opens a live tail. When replayFrom is set, matching history from
// that time is replayed (oldest first) before the live events. It invokes fn
// for each event until fn returns an error, the context is cancelled, or the
// server closes the stream; a clean server close returns nil.
func (c *Client) Subscribe(ctx context.Context, f Filter, replayFrom *time.Time, replayLimit int, fn func(Event) error) error {
	req := &talosv1.SubscribeRequest{
		ActorId:      f.ActorID,
		ResourceType: f.ResourceType,
		ResourceId:   f.ResourceID,
		Action:       f.Action,
		ReplayLimit:  uint32(replayLimit),
	}
	if replayFrom != nil {
		req.ReplayFrom = timestamppb.New(*replayFrom)
	}
	stream, err := c.talos.Subscribe(ctx, req)
	if err != nil {
		return apierrors.FromGRPCError(err)
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return apierrors.FromGRPCError(err)
		}
		if err := fn(eventFromProto(resp.GetEvent())); err != nil {
			return err
		}
	}
}

func eventToProto(e Event) *talosv1.AuditEvent {
	out := &talosv1.AuditEvent{
		Id:           e.ID,
		RealmId:      e.RealmID,
		ActorId:      e.ActorID,
		ActorType:    e.ActorType,
		ResourceType: e.ResourceType,
		ResourceId:   e.ResourceID,
		Action:       e.Action,
		Summary:      e.Summary,
		Ip:           e.IP,
		RequestId:    e.RequestID,
	}
	if !e.Timestamp.IsZero() {
		out.Timestamp = timestamppb.New(e.Timestamp)
	}
	out.Changes = mapToStruct(e.Changes)
	out.Metadata = mapToStruct(e.Metadata)
	return out
}

func eventFromProto(e *talosv1.AuditEvent) Event {
	out := Event{
		ID:           e.GetId(),
		RealmID:      e.GetRealmId(),
		ActorID:      e.GetActorId(),
		ActorType:    e.GetActorType(),
		ResourceType: e.GetResourceType(),
		ResourceID:   e.GetResourceId(),
		Action:       e.GetAction(),
		Summary:      e.GetSummary(),
		IP:           e.GetIp(),
		RequestID:    e.GetRequestId(),
	}
	if ts := e.GetTimestamp(); ts.IsValid() {
		out.Timestamp = ts.AsTime()
	}
	if s := e.GetChanges(); s != nil {
		out.Changes = s.AsMap()
	}
	if s := e.GetMetadata(); s != nil {
		out.Metadata = s.AsMap()
	}
	return out
}

func mapToStruct(m map[string]any) *structpb.Struct {
	if len(m) == 0 {
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		return nil
	}
	return s
}
