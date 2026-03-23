package mcp

import (
	"net/http"
	"time"

	"github.com/cordum/cordum/core/infra/httputil"
)

// SafeHTTPClient returns an *http.Client with redirect and SSRF protection.
func SafeHTTPClient(timeout time.Duration) *http.Client {
	return httputil.SafeClient(timeout)
}
