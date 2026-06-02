package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/app/apptest"
)

func TestRetentionSweep_DropsOnlyExpiredPartitions(t *testing.T) {
	store := apptest.NewPartitionStore(t)
	sweeper := app.NewRetentionSweeper(store)

	store.EXPECT().ListMonthlyPartitions(mock.Anything).Return([]string{
		"audit_event_2020_01", // upper 2020-02-01 — expired
		"audit_event_2020_12", // upper 2021-01-01 — exactly at cutoff, expired
		"audit_event_2026_06", // upper 2026-07-01 — retained
	}, nil)
	// Only the two pre-cutoff partitions are dropped; the recent one is left.
	store.EXPECT().DropPartition(mock.Anything, "audit_event_2020_01").Return(nil)
	store.EXPECT().DropPartition(mock.Anything, "audit_event_2020_12").Return(nil)

	before := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	n, err := sweeper.Sweep(context.Background(), before)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestProvision_CreatesCurrentAndNextMonth(t *testing.T) {
	store := apptest.NewPartitionStore(t)
	provisioner := app.NewPartitionProvisioner(store)

	// Neither partition exists yet, so both are created.
	store.EXPECT().ListMonthlyPartitions(mock.Anything).Return([]string{}, nil).Times(2)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	store.EXPECT().EnsureMonthlyPartition(mock.Anything,
		time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)).Return(nil)
	store.EXPECT().EnsureMonthlyPartition(mock.Anything,
		time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)).Return(nil)

	created, err := provisioner.Provision(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, 2, created)
}

func TestProvision_CountsOnlyNewlyCreated(t *testing.T) {
	store := apptest.NewPartitionStore(t)
	provisioner := app.NewPartitionProvisioner(store)

	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// The current month already exists; only next month is newly created.
	store.EXPECT().ListMonthlyPartitions(mock.Anything).
		Return([]string{"audit_event_2026_07"}, nil).Times(2)
	store.EXPECT().EnsureMonthlyPartition(mock.Anything, mock.Anything).Return(nil).Times(2)

	created, err := provisioner.Provision(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, 1, created, "only the previously-absent next month counts as created")
}

func TestProvision_CrossesYearBoundary(t *testing.T) {
	store := apptest.NewPartitionStore(t)
	provisioner := app.NewPartitionProvisioner(store)

	now := time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)
	store.EXPECT().ListMonthlyPartitions(mock.Anything).Return([]string{}, nil).Times(2)
	// December 2026 plus January 2027.
	store.EXPECT().EnsureMonthlyPartition(mock.Anything,
		time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)).Return(nil)
	store.EXPECT().EnsureMonthlyPartition(mock.Anything,
		time.Date(2027, 1, 20, 0, 0, 0, 0, time.UTC)).Return(nil)

	created, err := provisioner.Provision(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, 2, created)
}

func TestMonthlyPartitionName(t *testing.T) {
	// Local helper mirrors the DB naming; exercise it through Provision's
	// existence check by asserting the canonical name is matched.
	cases := map[time.Time]string{
		time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC):  "audit_event_2026_01",
		time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC): "audit_event_2026_12",
	}
	for in, want := range cases {
		store := apptest.NewPartitionStore(t)
		provisioner := app.NewPartitionProvisioner(store)
		// Report the wanted name as existing → that month is not counted as created.
		store.EXPECT().ListMonthlyPartitions(mock.Anything).Return([]string{want}, nil).Times(2)
		store.EXPECT().EnsureMonthlyPartition(mock.Anything, mock.Anything).Return(nil).Times(2)

		created, err := provisioner.Provision(context.Background(), in)
		require.NoError(t, err)
		assert.Equal(t, 1, created, "the matching month %s is treated as existing", want)
	}
}
