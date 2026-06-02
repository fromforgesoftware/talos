// Command drainer forwards audit events from a producer's outbox (e.g. aegis)
// to Talos. It claims rows from the configured outbox table and Appends each
// audit event to Talos over gRPC, keeping producers decoupled from Talos.
//
// Config (env):
//   OUTBOX_DB_URL    Postgres URL of the producer DB holding the outbox table.
//   OUTBOX_TABLE     Fully-qualified outbox table (default "aegis.outbox").
//   TALOS_GRPC_ADDR  Talos gRPC address (default "talos:9090").
//   DRAIN_INTERVAL   Poll interval (default 5s).
package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/fromforgesoftware/go-kit/audit"
	"github.com/fromforgesoftware/go-kit/monitoring"
	"github.com/fromforgesoftware/go-kit/monitoring/logger"
	"github.com/fromforgesoftware/go-kit/monitoring/tracer"
	"github.com/fromforgesoftware/go-kit/outbox"
	outboxpg "github.com/fromforgesoftware/go-kit/outbox/postgres"
	"github.com/fromforgesoftware/go-kit/persistence/gormdb/gormpg"

	talosaudit "github.com/fromforgesoftware/talos/audit"
	talosclient "github.com/fromforgesoftware/talos/pkg/client"
)

func main() {
	log := logger.New()
	ctx := context.Background()

	dbURL, err := url.Parse(os.Getenv("OUTBOX_DB_URL"))
	if err != nil {
		log.Error("invalid OUTBOX_DB_URL", "error", err)
		os.Exit(1)
	}
	tr, err := tracer.New()
	if err != nil {
		log.Error("tracer init failed", "error", err)
		os.Exit(1)
	}
	mon := monitoring.New(log, tr)

	db, err := gormpg.NewClient(dbURL, mon)
	if err != nil {
		log.Error("db connect failed", "error", err)
		os.Exit(1)
	}

	conn, err := grpc.NewClient(envOr("TALOS_GRPC_ADDR", "talos:9090"),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("talos dial failed", "error", err)
		os.Exit(1)
	}
	defer conn.Close()
	sink := talosaudit.NewSink(talosclient.New(conn))

	repo := outboxpg.New(db, envOr("OUTBOX_TABLE", "aegis.outbox"))
	handlers := map[string]outbox.Handler{
		"audit": outbox.HandlerFunc(func(ctx context.Context, msg outbox.Message) error {
			var e audit.Event
			if err := json.Unmarshal(msg.Payload, &e); err != nil {
				return err
			}
			return sink.Emit(ctx, e)
		}),
	}
	drainer := outbox.NewDrainer(repo, handlers, mon, outbox.Config{})

	interval := 5 * time.Second
	if d, err := time.ParseDuration(os.Getenv("DRAIN_INTERVAL")); err == nil && d > 0 {
		interval = d
	}
	log.Info("talos drainer started", "table", envOr("OUTBOX_TABLE", "aegis.outbox"), "interval", interval.String())
	for {
		if n, err := drainer.Drain(ctx); err != nil {
			log.Error("drain failed", "error", err)
		} else if n > 0 {
			log.Info("drained audit events", "count", n)
		}
		time.Sleep(interval)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
