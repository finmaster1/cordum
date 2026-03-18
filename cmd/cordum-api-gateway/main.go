package main

import (
	"log/slog"
	"os"

	"github.com/cordum/cordum/core/controlplane/gateway"
	"github.com/cordum/cordum/core/infra/buildinfo"
	"github.com/cordum/cordum/core/infra/config"
	"github.com/cordum/cordum/core/infra/logging"
)

func main() {
	logging.Init("gateway")
	slog.Info("cordum api gateway starting...")
	buildinfo.Log("cordum-api-gateway")
	cfg := config.Load()
	if err := gateway.Run(cfg); err != nil {
		slog.Error("api gateway error", "error", err)
		os.Exit(1)
	}
}
