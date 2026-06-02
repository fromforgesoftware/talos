// Command server boots the Talos audit/event-stream service: the kit's
// REST gateway (OpenAPI 3.1) plus a gRPC server exposing TalosService.
package main

import (
	"github.com/fromforgesoftware/go-kit/app"
	"github.com/fromforgesoftware/go-kit/openapi"
	"github.com/fromforgesoftware/go-kit/persistence/gormdb/gormpg"
	kitgrpc "github.com/fromforgesoftware/go-kit/transport/grpc"

	"github.com/fromforgesoftware/talos/internal"
)

func main() {
	app.Run(
		app.WithName("talos"),
		app.WithVersion(internal.Version),
		app.WithOpenAPI(
			openapi.SpecTitle("Talos"),
			openapi.SpecVersion(internal.Version),
			openapi.SpecDescription("Forge audit / event-stream service."),
		),
		gormpg.FxModule(),
		kitgrpc.FxModule(),
		internal.FxModule(),
	)
}
