// Command migrator applies Talos's database migrations and exits.
//
//	DB_* env vars configure the connection (see kit/persistence/sqldb).
//	go run ./cmd/migrator
package main

import (
	"context"
	"embed"

	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/lib/pq"

	"github.com/fromforgesoftware/go-kit/migrator"
)

//go:embed migrations
var migrationsFS embed.FS

func main() {
	if err := migrator.Up(context.Background(), migrationsFS, migrator.WithServiceName("talos")); err != nil {
		panic(err)
	}
}
