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
	"github.com/fromforgesoftware/talos/internal/internaltest"
)

func TestRetention_DropsExpiredPartition(t *testing.T) {
	client := internaltest.GetDB(t)
	t.Cleanup(func() {
		client.Exec(`DROP TABLE IF EXISTS talos.audit_event_2020_01`)
		internaltest.TruncateTables(t, client)
	})

	ctx := context.Background()
	require.NoError(t, client.WithContext(ctx).Exec(
		`CREATE TABLE talos.audit_event_2020_01 PARTITION OF talos.audit_event
		 FOR VALUES FROM ('2020-01-01 00:00:00+00') TO ('2020-02-01 00:00:00+00')`).Error)

	repo := db.NewPartitionRepository(client)
	before, err := repo.ListMonthlyPartitions(ctx)
	require.NoError(t, err)
	assert.Contains(t, before, "audit_event_2020_01")

	dropped, err := app.NewRetentionSweeper(repo).Sweep(ctx, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, dropped, 1)

	after, err := repo.ListMonthlyPartitions(ctx)
	require.NoError(t, err)
	assert.NotContains(t, after, "audit_event_2020_01", "expired partition dropped")
}
