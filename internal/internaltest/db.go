//go:build integration

package internaltest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fromforgesoftware/go-kit/migrator"
	"github.com/fromforgesoftware/go-kit/persistence/gormdb"
	"github.com/fromforgesoftware/go-kit/persistence/gormdb/gormdbtest"
	"github.com/stretchr/testify/require"
)

// GetDB returns a per-process singleton Postgres (via the kit's gormdbtest
// container helper) with Talos's migrations applied by the real kit
// migrator. We apply migrations with migrator.Up (golang-migrate, ordered)
// rather than gormdbtest's own option, which runs files in reverse.
// DB_SCHEMA=talos mirrors prod; the common-pre-migration bootstrap creates
// the talos schema before golang-migrate's tracking table needs it.
func GetDB(t *testing.T) *gormdb.DBClient {
	t.Helper()

	tdb := gormdbtest.GetDB(t, "talos")
	if tdb == nil {
		t.Skip("test database unavailable (docker/gnomock); skipping integration test")
	}

	t.Setenv("DB_HOST", tdb.Host)
	t.Setenv("DB_PORT", fmt.Sprintf("%d", tdb.Port))
	t.Setenv("DB_USER", tdb.User)
	t.Setenv("DB_PASSWORD", tdb.Password)
	t.Setenv("DB_NAME", tdb.DBName)
	t.Setenv("DB_SSL", "disable")
	t.Setenv("DB_SCHEMA", "talos")

	require.NoError(t, migrator.Up(context.Background(), os.DirFS(migratorDir()), migrator.WithServiceName("talos")))
	return tdb.DBClient
}

// TruncateTables wipes Talos's tables between tests sharing the singleton
// container.
func TruncateTables(t *testing.T, db *gormdb.DBClient) {
	t.Helper()
	require.NoError(t, db.Exec(`TRUNCATE TABLE talos.audit_event RESTART IDENTITY CASCADE;`).Error)
}

// migratorDir resolves services/talos/cmd/migrator relative to this source
// file, independent of the test's working directory.
func migratorDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "cmd", "migrator")
}
