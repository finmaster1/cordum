package main

import (
	"fmt"
	"os"
)

// runShadowCmd dispatches `cordumctl shadow <subcommand>`. EDGE-141 +
// EDGE-142 will add finding-store and remediation-hint subcommands as
// siblings of scan; the dispatch table here is intentionally small to
// keep the surface auditable.
func runShadowCmd(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: cordumctl shadow <scan>")
		return 2
	}
	switch args[0] {
	case "scan":
		return runShadowScanCmd(args[1:], os.Stdout, os.Stderr)
	default:
		fmt.Fprintf(os.Stderr, "unknown shadow subcommand %q\n", args[0])
		return 2
	}
}
