//go:build !windows

package llmchat

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// ListenSIGHUP installs a SIGHUP handler that calls
// loader.RefreshAll() on every signal receipt. On a fresh install the
// operator can edit a curated MD file and `kill -HUP $(pidof cordum-
// llm-chat)` to invalidate the cache without a full restart.
//
// The handler runs for the lifetime of the process; signal.Stop is
// not called because the substituter pack must stay refreshable until
// shutdown. Multiple invocations are safe but redundant — call once
// from main.go after the loader is constructed.
func ListenSIGHUP(loader *KnowledgePackLoader) {
	if loader == nil {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		for range ch {
			slog.Info("llmchat/knowledgepack: sighup_received")
			loader.RefreshAll()
		}
	}()
}
