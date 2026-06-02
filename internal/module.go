// Package internal wires Talos's components into a single fx module
// that cmd/server composes alongside the kit's defaults.
package internal

import (
	"context"
	"os"
	"strconv"
	"time"

	"go.uber.org/fx"

	"github.com/fromforgesoftware/go-kit/auth/jwt"
	"github.com/fromforgesoftware/go-kit/monitoring/logger"
	kitgrpc "github.com/fromforgesoftware/go-kit/transport/grpc"
	kitrest "github.com/fromforgesoftware/go-kit/transport/rest"

	"github.com/fromforgesoftware/talos/internal/app"
	"github.com/fromforgesoftware/talos/internal/db"
	talosgrpc "github.com/fromforgesoftware/talos/internal/transport/grpc"
	taloshttp "github.com/fromforgesoftware/talos/internal/transport/http"
)

// Version is the running Talos version; matches the published image tag.
const Version = "0.1.0"

// retentionInterval is how often the retention sweeper drops expired
// partitions; it only runs when TALOS_RETENTION_MONTHS > 0.
const retentionInterval = 6 * time.Hour

func FxModule() fx.Option {
	return fx.Module("talos",
		repositoriesFxModule(),
		usecasesFxModule(),
		transportFxModule(),
	)
}

func repositoriesFxModule() fx.Option {
	return fx.Module("talos:repositories",
		fx.Provide(
			fx.Annotate(db.NewAuditEventRepository, fx.As(new(app.AuditEventRepository))),
			fx.Annotate(db.NewPartitionRepository, fx.As(new(app.PartitionStore))),
		),
	)
}

// newFanout wires the built-in forwarders plus the live-tail broker. Real
// sinks (Kafka/NATS/S3/SIEM) register here as they're added.
func newFanout(broker *app.Broker) *app.Fanout {
	return app.NewFanout(app.NewLogForwarder(), broker)
}

func newAuditEventUsecase(events app.AuditEventRepository, fanout *app.Fanout, broker *app.Broker) app.AuditEventUsecase {
	return app.NewAuditEventUsecase(events, app.WithFanout(fanout), app.WithBroker(broker))
}

func usecasesFxModule() fx.Option {
	return fx.Module("talos:usecases",
		fx.Provide(
			app.NewBroker,
			newFanout,
			newAuditEventUsecase,
			fx.Annotate(app.NewRetentionSweeper, fx.As(new(app.RetentionSweeper))),
		),
	)
}

// newStreamValidator enables WebSocket live-tail auth when
// TALOS_STREAM_HMAC_SECRET is set: the bearer token must be a valid HMAC JWT
// signed with that shared secret. Unset → nil → auth disabled (dev default).
func newStreamValidator() jwt.Validator {
	secret := os.Getenv("TALOS_STREAM_HMAC_SECRET")
	if secret == "" {
		return nil
	}
	v, err := jwt.NewHMACIssuer(secret)
	if err != nil {
		return nil
	}
	return v
}

func transportFxModule() fx.Option {
	return fx.Module("talos:transport",
		kitrest.NewFxMiddleware(kitrest.NewGatewayMiddleware),
		fx.Invoke(registerRetentionSweeper),
		fx.Provide(newStreamValidator),
		kitgrpc.NewFxController(talosgrpc.NewTalosController),
		kitrest.NewFxController(taloshttp.NewAuditEventController),
	)
}

// retentionMonths reads TALOS_RETENTION_MONTHS; 0 (default/unset/invalid)
// disables retention.
func retentionMonths() int {
	n, err := strconv.Atoi(os.Getenv("TALOS_RETENTION_MONTHS"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// registerRetentionSweeper runs the partition-drop sweeper on an interval when
// retention is configured; OnStop cancels the loop.
func registerRetentionSweeper(lc fx.Lifecycle, sweeper app.RetentionSweeper) {
	months := retentionMonths()
	if months == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	log := logger.New()
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go runRetentionSweeper(ctx, sweeper, months, log)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

func runRetentionSweeper(ctx context.Context, sweeper app.RetentionSweeper, months int, log logger.Logger) {
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			before := time.Now().AddDate(0, -months, 0)
			if _, err := sweeper.Sweep(ctx, before); err != nil {
				log.ErrorContext(ctx, "retention sweep failed", "error", err)
			}
		}
	}
}
