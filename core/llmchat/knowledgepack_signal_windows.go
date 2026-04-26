//go:build windows

package llmchat

// ListenSIGHUP is a no-op on Windows — there is no SIGHUP signal
// class. Operators on Windows refresh the knowledge pack by
// restarting the service. main.go calls this unconditionally; the
// build tag selects the right implementation per target OS.
func ListenSIGHUP(_ *KnowledgePackLoader) {}
