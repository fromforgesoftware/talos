package audit_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kitaudit "github.com/fromforgesoftware/go-kit/audit"

	"github.com/fromforgesoftware/talos/audit"
	talos "github.com/fromforgesoftware/talos/pkg/client"
)

type captureClient struct {
	last talos.Event
}

func (c *captureClient) Append(_ context.Context, e talos.Event) (string, error) {
	c.last = e
	return "evt-1", nil
}

func TestSink_MapsEvent(t *testing.T) {
	client := &captureClient{}
	sink := audit.NewSink(client)

	err := sink.Emit(context.Background(), kitaudit.Event{
		Action:       "role.create",
		ResourceType: "role",
		ResourceID:   "role-1",
		ActorID:      "acc-1",
		ActorType:    "ACCOUNT",
		Summary:      "created role editor",
	})
	require.NoError(t, err)
	assert.Equal(t, "role.create", client.last.Action)
	assert.Equal(t, "role-1", client.last.ResourceID)
	assert.Equal(t, "acc-1", client.last.ActorID)
	assert.Equal(t, "created role editor", client.last.Summary)
}
