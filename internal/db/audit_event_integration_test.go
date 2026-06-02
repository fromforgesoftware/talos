//go:build integration

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/db"
	"github.com/fromforgesoftware/talos/internal/domain"
	"github.com/fromforgesoftware/talos/internal/internaltest"
)

func TestAuditEventAppendAndQuery(t *testing.T) {
	client := internaltest.GetDB(t)
	t.Cleanup(func() { internaltest.TruncateTables(t, client) })

	ctx := context.Background()
	repo, err := db.NewAuditEventRepository(client)
	require.NoError(t, err)

	base := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	created, err := repo.Create(ctx, internaltest.NewAuditEvent(
		internaltest.WithAETimestamp(base),
		internaltest.WithAEActor("actor-a", "user"),
		internaltest.WithAEResource("doc", "doc-1"),
		internaltest.WithAEAction("doc.created"),
		internaltest.WithAESummary("created a doc"),
		internaltest.WithAEMetadata(map[string]any{"region": "eu", "tier": "gold"}),
	))
	require.NoError(t, err)
	require.NotEmpty(t, created.ID())

	_, err = repo.Create(ctx, internaltest.NewAuditEvent(
		internaltest.WithAETimestamp(base.Add(time.Hour)),
		internaltest.WithAEActor("actor-b", "service"),
		internaltest.WithAEResource("trade", "trade-7"),
		internaltest.WithAEAction("trade.placed"),
	))
	require.NoError(t, err)

	// Default timestamp/id: leaving them zero falls back to table defaults.
	defaulted, err := repo.Create(ctx, domain.NewAuditEvent("login.success",
		domain.WithAuditEventActor("actor-a", "user")))
	require.NoError(t, err)
	require.NotEmpty(t, defaulted.ID())
	assert.False(t, defaulted.Timestamp().IsZero())

	t.Run("filter by actor", func(t *testing.T) {
		list, err := repo.Query(ctx, app.AuditEventFilter{ActorID: "actor-a"})
		require.NoError(t, err)
		assert.Equal(t, 2, list.TotalCount())
	})

	t.Run("filter by resource", func(t *testing.T) {
		list, err := repo.Query(ctx, app.AuditEventFilter{ResourceType: "doc", ResourceID: "doc-1"})
		require.NoError(t, err)
		require.Len(t, list.Results(), 1)
		assert.Equal(t, "doc.created", list.Results()[0].Action())
	})

	t.Run("filter by action", func(t *testing.T) {
		list, err := repo.Query(ctx, app.AuditEventFilter{Action: "trade.placed"})
		require.NoError(t, err)
		require.Len(t, list.Results(), 1)
		assert.Equal(t, "actor-b", list.Results()[0].ActorID())
	})

	t.Run("filter by time range", func(t *testing.T) {
		from := base.Add(-time.Minute)
		to := base.Add(time.Minute)
		list, err := repo.Query(ctx, app.AuditEventFilter{From: &from, To: &to})
		require.NoError(t, err)
		require.Len(t, list.Results(), 1)
		assert.Equal(t, "doc.created", list.Results()[0].Action())
	})

	t.Run("metadata round-trips", func(t *testing.T) {
		list, err := repo.Query(ctx, app.AuditEventFilter{ResourceType: "doc"})
		require.NoError(t, err)
		require.Len(t, list.Results(), 1)
		assert.Equal(t, "eu", list.Results()[0].Metadata()["region"])
	})

	t.Run("newest first", func(t *testing.T) {
		list, err := repo.Query(ctx, app.AuditEventFilter{ActorID: "actor-a"})
		require.NoError(t, err)
		require.Len(t, list.Results(), 2)
		assert.True(t, list.Results()[0].Timestamp().After(list.Results()[1].Timestamp()))
	})
}

func TestAuditEventAppendOnly(t *testing.T) {
	client := internaltest.GetDB(t)
	t.Cleanup(func() { internaltest.TruncateTables(t, client) })

	ctx := context.Background()
	repo, err := db.NewAuditEventRepository(client)
	require.NoError(t, err)

	created, err := repo.Create(ctx, internaltest.NewAuditEvent(
		internaltest.WithAETimestamp(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		internaltest.WithAEAction("doc.created"),
	))
	require.NoError(t, err)

	// The BEFORE UPDATE/DELETE trigger raises, so both are rejected at the DB.
	updErr := client.WithContext(ctx).
		Exec(`UPDATE talos.audit_event SET action = ? WHERE id = ?`, "tampered", created.ID()).Error
	require.Error(t, updErr)
	assert.Contains(t, updErr.Error(), "append-only")

	delErr := client.WithContext(ctx).
		Exec(`DELETE FROM talos.audit_event WHERE id = ?`, created.ID()).Error
	require.Error(t, delErr)
	assert.Contains(t, delErr.Error(), "append-only")
}
