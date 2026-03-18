package main

import (
	"log/slog"
	"os"

	"github.com/cordum/cordum/core/controlplane/safetykernel"
	"github.com/cordum/cordum/core/infra/buildinfo"
	"github.com/cordum/cordum/core/infra/config"
	"github.com/cordum/cordum/core/infra/logging"
)

func main() {
	logging.Init("safety-kernel")
	slog.Info("cordum safety kernel starting...")
	buildinfo.Log("cordum-safety-kernel")
	cfg := config.Load()
	if err := safetykernel.Run(cfg); err != nil {
		slog.Error("safety-kernel error", "error", err)
		os.Exit(1)
	}
}
