package db

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/fromforgesoftware/go-kit/persistence/gormdb"
	"github.com/fromforgesoftware/go-kit/persistence/postgres"
)

// partitionName guards DDL: partition identifiers come from the system
// catalog and must match the monthly naming before being interpolated.
var partitionName = regexp.MustCompile(`^audit_event_[0-9]{4}_[0-9]{2}$`)

type partitionRepo struct {
	db *gormdb.DBClient
}

func NewPartitionRepository(db *gormdb.DBClient) *partitionRepo {
	return &partitionRepo{db: db}
}

// ListMonthlyPartitions returns the monthly child partitions of
// talos.audit_event (the catch-all default partition is excluded).
func (r *partitionRepo) ListMonthlyPartitions(ctx context.Context) ([]string, error) {
	var names []string
	err := r.db.WithContext(ctx).Raw(`
		SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent
		JOIN pg_namespace n ON n.oid = p.relnamespace
		WHERE n.nspname = 'talos' AND p.relname = 'audit_event'
	`).Scan(&names).Error
	if err != nil {
		return nil, postgres.NewErrUnknown(err)
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if partitionName.MatchString(name) {
			out = append(out, name)
		}
	}
	return out, nil
}

// DropPartition detaches and drops a monthly partition. The name is validated
// against the monthly pattern before interpolation.
func (r *partitionRepo) DropPartition(ctx context.Context, name string) error {
	if !partitionName.MatchString(name) {
		return postgres.NewErrUnknown(errInvalidPartition{name})
	}
	if err := r.db.WithContext(ctx).Exec("DROP TABLE IF EXISTS talos." + name).Error; err != nil {
		return postgres.NewErrUnknown(err)
	}
	return nil
}

// EnsureMonthlyPartition creates the monthly partition covering month (any time
// within the target UTC month) if it does not already exist. It is a no-op when
// the partition is present, so it is safe to call repeatedly at startup and on
// the retention ticker. The CREATE ... PARTITION OF carries explicit FROM/TO
// bounds for the month so rows land here rather than in the DEFAULT catch-all.
//
// Identifiers and bounds are derived from a validated name and time.Time (never
// from user input), so the interpolation cannot inject DDL.
func (r *partitionRepo) EnsureMonthlyPartition(ctx context.Context, month time.Time) error {
	month = month.UTC()
	start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	name := fmt.Sprintf("audit_event_%04d_%02d", start.Year(), int(start.Month()))
	if !partitionName.MatchString(name) {
		return postgres.NewErrUnknown(errInvalidPartition{name})
	}
	// IF NOT EXISTS keeps this idempotent and free of a check-then-create race.
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS talos.%s PARTITION OF talos.audit_event FOR VALUES FROM ('%s') TO ('%s')",
		name,
		start.Format("2006-01-02 15:04:05-07"),
		end.Format("2006-01-02 15:04:05-07"),
	)
	if err := r.db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return postgres.NewErrUnknown(err)
	}
	return nil
}

type errInvalidPartition struct{ name string }

func (e errInvalidPartition) Error() string { return "invalid partition name: " + e.name }
