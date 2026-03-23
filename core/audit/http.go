package audit

import (
	"net/http"
	"time"

	"github.com/cordum/cordum/core/infra/httputil"
)

// safeHTTPClient returns an *http.Client with redirect and SSRF protection.
func safeHTTPClient(timeout time.Duration) *http.Client {
	return httputil.SafeClient(timeout)
}
