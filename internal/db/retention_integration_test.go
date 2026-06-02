//go:build integration

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fromforgesoftware/go-kit/persistence/gormdb"

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

// TestPartitionProvisioning_RoutesAcrossMonthBoundaries verifies the
// application-managed partition lifecycle end to end against real Postgres:
// provisioning creates the right monthly partitions, events inserted across a
// month boundary land in the correct child partition (never the DEFAULT
// catch-all), and retention then drops the older month while keeping the newer.
func TestPartitionProvisioning_RoutesAcrossMonthBoundaries(t *testing.T) {
	client := internaltest.GetDB(t)
	t.Cleanup(func() {
		client.Exec(`DROP TABLE IF EXISTS talos.audit_event_2027_03`)
		client.Exec(`DROP TABLE IF EXISTS talos.audit_event_2027_04`)
		internaltest.TruncateTables(t, client)
	})

	ctx := context.Background()
	repo := db.NewPartitionRepository(client)
	provisioner := app.NewPartitionProvisioner(repo)

	// "now" is mid-March 2027; provisioning must create March and April.
	now := time.Date(2027, 3, 10, 12, 0, 0, 0, time.UTC)
	created, err := provisioner.Provision(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, 2, created, "March and April 2027 partitions are created")

	// Idempotent: a second pass creates nothing.
	created, err = provisioner.Provision(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, 0, created)

	names, err := repo.ListMonthlyPartitions(ctx)
	require.NoError(t, err)
	assert.Contains(t, names, "audit_event_2027_03")
	assert.Contains(t, names, "audit_event_2027_04")

	// Insert one event in each month; assert each lands in its own partition and
	// nothing falls into the DEFAULT catch-all.
	require.NoError(t, client.WithContext(ctx).Exec(
		`INSERT INTO talos.audit_event (timestamp, action) VALUES (?, ?)`,
		time.Date(2027, 3, 15, 0, 0, 0, 0, time.UTC), "march.event").Error)
	require.NoError(t, client.WithContext(ctx).Exec(
		`INSERT INTO talos.audit_event (timestamp, action) VALUES (?, ?)`,
		time.Date(2027, 4, 15, 0, 0, 0, 0, time.UTC), "april.event").Error)

	assert.Equal(t, int64(1), countIn(t, client, "audit_event_2027_03"))
	assert.Equal(t, int64(1), countIn(t, client, "audit_event_2027_04"))
	assert.Equal(t, int64(0), countIn(t, client, "audit_event_default"),
		"provisioned partitions absorb events; the DEFAULT catch-all stays empty")

	// Retention with a cutoff inside April drops March (fully expired) and keeps
	// April, exercising the drop path on application-created partitions.
	dropped, err := app.NewRetentionSweeper(repo).Sweep(ctx,
		time.Date(2027, 4, 10, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, dropped, 1)

	after, err := repo.ListMonthlyPartitions(ctx)
	require.NoError(t, err)
	assert.NotContains(t, after, "audit_event_2027_03", "expired March partition dropped")
	assert.Contains(t, after, "audit_event_2027_04", "current April partition retained")
}

// countIn returns the row count held directly by a child partition (queried by
// its own name, not the parent), so a test can assert which partition a row
// landed in.
func countIn(t *testing.T, client *gormdb.DBClient, partition string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, client.Raw(`SELECT count(*) FROM talos.`+partition).Scan(&n).Error)
	return n
}
