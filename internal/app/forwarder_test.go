package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/app/apptest"
	"github.com/fromforgesoftware/talos/internal/domain"
	"github.com/fromforgesoftware/talos/internal/internaltest"
)

type fakeForwarder struct {
	name  string
	calls int
	err   error
}

func (f *fakeForwarder) Name() string { return f.name }
func (f *fakeForwarder) Forward(context.Context, domain.AuditEvent) error {
	f.calls++
	return f.err
}

func TestFanout_DeliversToAll(t *testing.T) {
	a := &fakeForwarder{name: "a"}
	b := &fakeForwarder{name: "b"}
	app.NewFanout(a, b).Forward(context.Background(), internaltest.NewAuditEvent())
	assert.Equal(t, 1, a.calls)
	assert.Equal(t, 1, b.calls)
}

func TestFanout_ContinuesPastAFailure(t *testing.T) {
	failing := &fakeForwarder{name: "bad", err: errors.New("kafka down")}
	ok := &fakeForwarder{name: "good"}
	app.NewFanout(failing, ok).Forward(context.Background(), internaltest.NewAuditEvent())
	assert.Equal(t, 1, ok.calls, "a failing forwarder does not block the others")
}

func TestAppend_FansOut(t *testing.T) {
	events := apptest.NewAuditEventRepository(t)
	fwd := &fakeForwarder{name: "f"}
	uc := app.NewAuditEventUsecase(events, app.WithFanout(app.NewFanout(fwd)))

	stored := internaltest.NewAuditEvent(internaltest.WithAEID("e-1"))
	events.EXPECT().Create(mock.Anything, mock.Anything).Return(stored, nil)

	_, err := uc.Append(context.Background(), internaltest.NewAuditEvent(internaltest.WithAEAction("role.grant")))
	require.NoError(t, err)
	assert.Equal(t, 1, fwd.calls)
}
