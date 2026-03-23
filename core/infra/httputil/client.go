package httputil

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// MaxRedirects is the maximum number of HTTP redirects followed by SafeClient.
const MaxRedirects = 5

// SafeClient returns an *http.Client with redirect protection.
// It limits the total number of redirects and blocks redirects to
// non-HTTPS targets, preventing SSRF via open-redirect chains and
// redirect-loop denial-of-service.
func SafeClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", MaxRedirects)
			}
			if req.URL.Scheme != "https" {
				return errors.New("redirect to non-HTTPS target blocked")
			}
			return nil
		},
	}
}
