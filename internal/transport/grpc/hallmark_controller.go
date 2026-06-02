// Package grpc holds Talos's gRPC controllers. Each controller
// implements kitgrpc.Controller (SD + the service methods) and is
// registered with the kit's gRPC gateway via grpc.NewFxController.
package grpc

import (
	"context"
	"errors"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	kitgrpc "github.com/fromforgesoftware/go-kit/transport/grpc"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/domain"
	talosv1 "github.com/fromforgesoftware/talos/pkg/api/talos/v1"
)

type talosController struct {
	events app.AuditEventUsecase
}

// NewTalosController builds the TalosService controller. Returned
// errors are apierrors values; the kit's gRPC layer maps them to status
// codes.
func NewTalosController(events app.AuditEventUsecase) kitgrpc.Controller {
	return &talosController{events: events}
}

func (c *talosController) SD() kitgrpc.ServiceDesc {
	return &talosv1.TalosService_ServiceDesc
}

func (c *talosController) Append(ctx context.Context, req *talosv1.AppendRequest) (*talosv1.AppendResponse, error) {
	created, err := c.events.Append(ctx, eventFromProto(req.GetEvent()))
	if err != nil {
		return nil, err
	}
	return &talosv1.AppendResponse{Id: created.ID()}, nil
}

// AppendStream drains the client stream, appending each event, and returns
// the count persisted. A failed append aborts the stream so the client
// learns the batch did not fully land.
func (c *talosController) AppendStream(stream grpc.ClientStreamingServer[talosv1.AppendStreamRequest, talosv1.AppendStreamResponse]) error {
	var count uint32
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&talosv1.AppendStreamResponse{Count: count})
		}
		if err != nil {
			return err
		}
		if _, err := c.events.Append(stream.Context(), eventFromProto(req.GetEvent())); err != nil {
			return err
		}
		count++
	}
}

func (c *talosController) Query(ctx context.Context, req *talosv1.QueryRequest) (*talosv1.QueryResponse, error) {
	list, err := c.events.Query(ctx, app.AuditEventFilter{
		ActorID:      req.GetActorId(),
		ResourceType: req.GetResourceType(),
		ResourceID:   req.GetResourceId(),
		Action:       req.GetAction(),
		From:         timeOrNil(req.GetFrom()),
		To:           timeOrNil(req.GetTo()),
		Limit:        int(req.GetLimit()),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*talosv1.AuditEvent, 0, len(list.Results()))
	for _, e := range list.Results() {
		out = append(out, eventToProto(e))
	}
	return &talosv1.QueryResponse{Events: out, TotalCount: uint32(list.TotalCount())}, nil
}

// Subscribe replays matching history (oldest first) when replay_from is set,
// then streams live events until the client disconnects. Events seen during
// replay are skipped in the live phase so the boundary never duplicates.
func (c *talosController) Subscribe(req *talosv1.SubscribeRequest, stream grpc.ServerStreamingServer[talosv1.SubscribeResponse]) error {
	filter := app.AuditEventFilter{
		ActorID:      req.GetActorId(),
		ResourceType: req.GetResourceType(),
		ResourceID:   req.GetResourceId(),
		Action:       req.GetAction(),
	}
	ctx := stream.Context()

	// Attach to the live feed first so events appended during replay are
	// buffered rather than lost in the gap between replay and the live tail.
	sub, err := c.events.Subscribe(filter)
	if err != nil {
		return err
	}
	defer sub.Close()

	replayed := map[string]struct{}{}
	if from := timeOrNil(req.GetReplayFrom()); from != nil {
		list, err := c.events.Query(ctx, app.AuditEventFilter{
			ActorID:      filter.ActorID,
			ResourceType: filter.ResourceType,
			ResourceID:   filter.ResourceID,
			Action:       filter.Action,
			From:         from,
			Limit:        int(req.GetReplayLimit()),
		})
		if err != nil {
			return err
		}
		history := list.Results()
		for i := len(history) - 1; i >= 0; i-- { // Query is newest-first; replay oldest-first.
			if err := stream.Send(&talosv1.SubscribeResponse{Event: eventToProto(history[i])}); err != nil {
				return err
			}
			replayed[history[i].ID()] = struct{}{}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-sub.C:
			if !ok {
				return nil
			}
			if _, seen := replayed[event.ID()]; seen {
				continue
			}
			if err := stream.Send(&talosv1.SubscribeResponse{Event: eventToProto(event)}); err != nil {
				return err
			}
		}
	}
}

func eventFromProto(e *talosv1.AuditEvent) domain.AuditEvent {
	if e == nil {
		return domain.NewAuditEvent("")
	}
	opts := []domain.AuditEventOption{
		domain.WithAuditEventRealmID(e.GetRealmId()),
		domain.WithAuditEventActor(e.GetActorId(), e.GetActorType()),
		domain.WithAuditEventResource(e.GetResourceType(), e.GetResourceId()),
		domain.WithAuditEventSummary(e.GetSummary()),
		domain.WithAuditEventChanges(structToMap(e.GetChanges())),
		domain.WithAuditEventMetadata(structToMap(e.GetMetadata())),
		domain.WithAuditEventIP(e.GetIp()),
		domain.WithAuditEventRequestID(e.GetRequestId()),
	}
	if e.GetId() != "" {
		opts = append(opts, domain.WithAuditEventID(e.GetId()))
	}
	if t := timeOrNil(e.GetTimestamp()); t != nil {
		opts = append(opts, domain.WithAuditEventTimestamp(*t))
	}
	return domain.NewAuditEvent(e.GetAction(), opts...)
}

func eventToProto(e domain.AuditEvent) *talosv1.AuditEvent {
	out := &talosv1.AuditEvent{
		Id:           e.ID(),
		Timestamp:    timestamppb.New(e.Timestamp()),
		RealmId:      e.RealmID(),
		ActorId:      e.ActorID(),
		ActorType:    e.ActorType(),
		ResourceType: e.ResourceType(),
		ResourceId:   e.ResourceID(),
		Action:       e.Action(),
		Summary:      e.Summary(),
		Ip:           e.IP(),
		RequestId:    e.RequestID(),
	}
	out.Changes = mapToStruct(e.Changes())
	out.Metadata = mapToStruct(e.Metadata())
	return out
}

func timeOrNil(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil || !ts.IsValid() || ts.AsTime().IsZero() {
		return nil
	}
	t := ts.AsTime()
	return &t
}

func structToMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
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
