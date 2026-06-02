// Package internal wires Talos's components into a single fx module
// that cmd/server composes alongside the kit's defaults.
package internal

import (
	"context"
	"errors"
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

// maintenanceInterval is how often the partition-maintenance loop runs: it
// always provisions the current+next month's partitions, and additionally drops
// expired partitions when TALOS_RETENTION_MONTHS > 0.
const maintenanceInterval = 6 * time.Hour

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
			fx.Annotate(app.NewPartitionProvisioner, fx.As(new(app.PartitionProvisioner))),
		),
	)
}

// newStreamValidator enables WebSocket live-tail auth from
// TALOS_STREAM_HMAC_SECRET: the bearer token must be a valid HMAC JWT signed
// with that shared secret.
//
// Secure by default: the audit stream carries sensitive events, so when no
// secret is configured the service refuses to start. Local development can
// explicitly opt out of auth with TALOS_WS_ALLOW_INSECURE=1, which returns a
// nil validator (auth disabled) instead of an error.
func newStreamValidator() (jwt.Validator, error) {
	secret := os.Getenv("TALOS_STREAM_HMAC_SECRET")
	if secret == "" {
		if wsAllowInsecure() {
			return nil, nil // explicit dev opt-out: auth disabled
		}
		return nil, errInsecureStream
	}
	return jwt.NewHMACIssuer(secret)
}

// errInsecureStream blocks startup when the live-tail auth secret is missing and
// the operator has not explicitly opted into the insecure posture.
var errInsecureStream = errors.New(
	"TALOS_STREAM_HMAC_SECRET is required to secure the audit live-tail; " +
		"set it, or set TALOS_WS_ALLOW_INSECURE=1 to run without auth (development only)")

// wsAllowInsecure reports whether the operator has explicitly opted into the
// insecure WebSocket posture (no auth, all origins). Default false.
func wsAllowInsecure() bool {
	return os.Getenv("TALOS_WS_ALLOW_INSECURE") == "1"
}

func transportFxModule() fx.Option {
	return fx.Module("talos:transport",
		kitrest.NewFxMiddleware(kitrest.NewGatewayMiddleware),
		fx.Invoke(registerPartitionMaintenance),
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

// registerPartitionMaintenance keeps the monthly partitions healthy. It always
// provisions the current + next month's partitions (so new rows never fall into
// the DEFAULT catch-all), and additionally drops expired partitions when
// retention is configured. Both run once at startup (OnStart) and then on the
// maintenance ticker; OnStop cancels the loop.
func registerPartitionMaintenance(lc fx.Lifecycle, provisioner app.PartitionProvisioner, sweeper app.RetentionSweeper) {
	months := retentionMonths()
	ctx, cancel := context.WithCancel(context.Background())
	log := logger.New()
	lc.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			// Run an initial maintenance pass synchronously at startup so the
			// current partition exists before the first event is appended, and
			// any backlog of expired partitions is dropped immediately rather
			// than waiting up to maintenanceInterval for the first tick.
			runMaintenance(startCtx, provisioner, sweeper, months, log)
			go runMaintenanceLoop(ctx, provisioner, sweeper, months, log)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

func runMaintenanceLoop(ctx context.Context, provisioner app.PartitionProvisioner, sweeper app.RetentionSweeper, months int, log logger.Logger) {
	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runMaintenance(ctx, provisioner, sweeper, months, log)
		}
	}
}

// runMaintenance provisions upcoming partitions then sweeps expired ones.
// Provisioning runs unconditionally; the sweep only runs when retention is
// enabled (months > 0). Errors are logged, never fatal — a transient failure
// must not crash ingest.
func runMaintenance(ctx context.Context, provisioner app.PartitionProvisioner, sweeper app.RetentionSweeper, months int, log logger.Logger) {
	if _, err := provisioner.Provision(ctx, time.Now()); err != nil {
		log.ErrorContext(ctx, "partition provisioning failed", "error", err)
	}
	if months == 0 {
		return
	}
	before := time.Now().AddDate(0, -months, 0)
	if _, err := sweeper.Sweep(ctx, before); err != nil {
		log.ErrorContext(ctx, "retention sweep failed", "error", err)
	}
}
