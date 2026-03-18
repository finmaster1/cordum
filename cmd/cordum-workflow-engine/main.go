package main

import (
	"log/slog"
	"os"

	"github.com/cordum/cordum/core/infra/buildinfo"
	"github.com/cordum/cordum/core/infra/config"
	"github.com/cordum/cordum/core/infra/logging"
	"github.com/cordum/cordum/core/workflow"
)

func main() {
	logging.Init("workflow-engine")
	slog.Info("cordum workflow engine starting...")
	buildinfo.Log("cordum-workflow-engine")
	cfg := config.Load()
	if err := workflow.Run(cfg); err != nil {
		slog.Error("workflow engine error", "error", err)
		os.Exit(1)
	}
}
