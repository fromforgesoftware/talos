package app

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// PartitionStore lists, creates and drops the monthly audit-event partitions.
type PartitionStore interface {
	ListMonthlyPartitions(ctx context.Context) ([]string, error)
	DropPartition(ctx context.Context, name string) error
	// EnsureMonthlyPartition creates the partition for the month containing
	// the given instant if it is absent; it is idempotent.
	EnsureMonthlyPartition(ctx context.Context, month time.Time) error
}

// PartitionProvisioner ensures the monthly partitions that incoming events need
// already exist, so new rows never fall into the DEFAULT catch-all under normal
// operation.
type PartitionProvisioner interface {
	// Provision ensures the partitions for the month containing `now` and the
	// following month exist, returning how many it created.
	Provision(ctx context.Context, now time.Time) (int, error)
}

type partitionProvisioner struct {
	partitions PartitionStore
}

func NewPartitionProvisioner(partitions PartitionStore) PartitionProvisioner {
	return &partitionProvisioner{partitions: partitions}
}

// Provision ensures the current and next month's partitions exist. Next month
// is covered too so events that cross the boundary mid-tick (or arrive slightly
// ahead of the clock) still land in a real partition rather than DEFAULT.
func (p *partitionProvisioner) Provision(ctx context.Context, now time.Time) (int, error) {
	now = now.UTC()
	months := []time.Time{now, now.AddDate(0, 1, 0)}
	created := 0
	for _, m := range months {
		existed, err := p.partitionExists(ctx, m)
		if err != nil {
			return created, err
		}
		if err := p.partitions.EnsureMonthlyPartition(ctx, m); err != nil {
			return created, err
		}
		if !existed {
			created++
		}
	}
	return created, nil
}

// partitionExists reports whether the monthly partition for month already
// exists, so Provision can report an accurate created count (EnsureMonthly is
// idempotent and does not distinguish create-vs-noop on its own).
func (p *partitionProvisioner) partitionExists(ctx context.Context, month time.Time) (bool, error) {
	want := monthlyPartitionName(month)
	names, err := p.partitions.ListMonthlyPartitions(ctx)
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if n == want {
			return true, nil
		}
	}
	return false, nil
}

// monthlyPartitionName is the canonical audit_event_YYYY_MM name for a month.
func monthlyPartitionName(month time.Time) string {
	m := month.UTC()
	return "audit_event_" +
		strconv.Itoa(m.Year()) + "_" +
		zeroPad(int(m.Month()))
}

func zeroPad(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
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
