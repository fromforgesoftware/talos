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
