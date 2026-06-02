package app_test

import (
	"context"
	"testing"
	"time"

	apierrors "github.com/fromforgesoftware/go-kit/errors"
	"github.com/fromforgesoftware/go-kit/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/app/apptest"
	"github.com/fromforgesoftware/talos/internal/domain"
	"github.com/fromforgesoftware/talos/internal/internaltest"
)

func TestAppend_PersistsEvent(t *testing.T) {
	events := apptest.NewAuditEventRepository(t)
	uc := app.NewAuditEventUsecase(events)

	want := internaltest.NewAuditEvent(internaltest.WithAEAction("doc.updated"))
	events.EXPECT().Create(mock.Anything, mock.MatchedBy(internaltest.MatchAuditEvent(want))).
		Return(internaltest.NewAuditEvent(internaltest.WithAEID("evt-1"), internaltest.WithAEAction("doc.updated")), nil)

	got, err := uc.Append(context.Background(),
		internaltest.NewAuditEvent(internaltest.WithAEAction("doc.updated")))
	require.NoError(t, err)
	assert.Equal(t, "evt-1", got.ID())
}

func TestAppend_RejectsMissingAction(t *testing.T) {
	events := apptest.NewAuditEventRepository(t)
	uc := app.NewAuditEventUsecase(events)

	_, err := uc.Append(context.Background(), domain.NewAuditEvent(""))
	require.Error(t, err)
	assert.True(t, apierrors.Is(err, apierrors.CodeInvalidArgument))
}

func TestQuery_PassesFilterThrough(t *testing.T) {
	events := apptest.NewAuditEventRepository(t)
	uc := app.NewAuditEventUsecase(events)

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	want := app.AuditEventFilter{ActorID: "actor-1", ResourceType: "doc", Action: "doc.updated", From: &from, Limit: 25}
	events.EXPECT().Query(mock.Anything, mock.MatchedBy(func(f app.AuditEventFilter) bool {
		return f.ActorID == "actor-1" && f.ResourceType == "doc" &&
			f.Action == "doc.updated" && f.From != nil && f.From.Equal(from) && f.Limit == 25
	})).Return(resource.NewListResponse([]domain.AuditEvent{
		internaltest.NewAuditEvent(internaltest.WithAEID("evt-1")),
	}, 1), nil)

	list, err := uc.Query(context.Background(), want)
	require.NoError(t, err)
	require.Len(t, list.Results(), 1)
	assert.Equal(t, "evt-1", list.Results()[0].ID())
}

func TestFilterQueryOptions_DefaultsLimit(t *testing.T) {
	q := app.AuditEventFilter{}.QueryOptions()
	require.Len(t, q, 1)
}

func TestSubscribe_WithoutBrokerErrors(t *testing.T) {
	events := apptest.NewAuditEventRepository(t)
	uc := app.NewAuditEventUsecase(events)

	_, err := uc.Subscribe(app.AuditEventFilter{})
	require.Error(t, err)
	assert.True(t, apierrors.Is(err, apierrors.CodeServiceUnavailable))
}

func TestSubscribe_WithBrokerSeesAppend(t *testing.T) {
	events := apptest.NewAuditEventRepository(t)
	broker := app.NewBroker()
	uc := app.NewAuditEventUsecase(events, app.WithFanout(app.NewFanout(broker)), app.WithBroker(broker))

	created := internaltest.NewAuditEvent(internaltest.WithAEAction("doc.created"))
	events.EXPECT().Create(mock.Anything, mock.Anything).Return(created, nil)

	sub, err := uc.Subscribe(app.AuditEventFilter{Action: "doc.created"})
	require.NoError(t, err)
	defer sub.Close()

	_, err = uc.Append(context.Background(), created)
	require.NoError(t, err)

	select {
	case got := <-sub.C:
		assert.Equal(t, "doc.created", got.Action())
	case <-time.After(time.Second):
		t.Fatal("expected the appended event on the live feed")
	}
}
