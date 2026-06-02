package app

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// PartitionStore lists and drops the monthly audit-event partitions.
type PartitionStore interface {
	ListMonthlyPartitions(ctx context.Context) ([]string, error)
	DropPartition(ctx context.Context, name string) error
}

// RetentionSweeper drops monthly partitions whose entire month precedes the
// cutoff — TTL enforced by dropping whole partitions, not row deletes (the
// table is append-only).
type RetentionSweeper interface {
	Sweep(ctx context.Context, before time.Time) (int, error)
}

type retentionSweeper struct {
	partitions PartitionStore
}

func NewRetentionSweeper(partitions PartitionStore) RetentionSweeper {
	return &retentionSweeper{partitions: partitions}
}

func (s *retentionSweeper) Sweep(ctx context.Context, before time.Time) (int, error) {
	names, err := s.partitions.ListMonthlyPartitions(ctx)
	if err != nil {
		return 0, err
	}
	dropped := 0
	for _, name := range names {
		if !partitionExpired(name, before) {
			continue
		}
		if err := s.partitions.DropPartition(ctx, name); err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}

// partitionExpired reports whether the monthly partition's exclusive upper
// bound (first day of the following month) is at or before the cutoff — i.e.
// every row it holds predates `before`. Pure, so the boundary is unit-tested.
func partitionExpired(name string, before time.Time) bool {
	const prefix = "audit_event_"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(name, prefix), "_")
	if len(parts) != 2 {
		return false
	}
	year, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || month < 1 || month > 12 {
		return false
	}
	upper := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	return !upper.After(before)
}
