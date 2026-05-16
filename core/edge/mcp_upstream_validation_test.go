package edge

import (
	"context"
	"errors"
	"testing"
)

func TestRegistryValidationRejectsInvalidTransport(t *testing.T) {
	upstream := validMCPUpstream("tenant-a", "bad-transport")
	upstream.Transport = "websocket"
	err := Validate(context.Background(), &upstream, string(PolicyModeObserve), nil)
	if !errors.Is(err, ErrInvalidTransport) {
		t.Fatalf("Validate error = %v, want ErrInvalidTransport", err)
	}
}

func TestRegistryValidationAllowsHTTPOutsideStrict(t *testing.T) {
	upstream := validMCPUpstream("tenant-a", "local-dev")
	upstream.Endpoint = "http://localhost:8080"
	if err := Validate(context.Background(), &upstream, string(PolicyModeObserve), nil); err != nil {
		t.Fatalf("non-strict HTTP localhost should be valid for local-dev: %v", err)
	}
}

func TestRegistryValidationAllowsEmptyAuthSecretRef(t *testing.T) {
	upstream := validMCPUpstream("tenant-a", "no-auth")
	upstream.AuthSecretRef = ""
	if err := Validate(context.Background(), &upstream, string(PolicyModeObserve), nil); err != nil {
		t.Fatalf("empty AuthSecretRef should be valid for no-auth upstream: %v", err)
	}
}

func TestRegistryValidationRejectsInvalidNameAndRisk(t *testing.T) {
	badName := validMCPUpstream("tenant-a", "../etc/passwd")
	if err := Validate(context.Background(), &badName, string(PolicyModeObserve), nil); !errors.Is(err, ErrInvalidUpstream) {
		t.Fatalf("bad name error = %v, want ErrInvalidUpstream", err)
	}

	badRisk := validMCPUpstream("tenant-a", "bad-risk")
	badRisk.Risk = "maximum"
	if err := Validate(context.Background(), &badRisk, string(PolicyModeObserve), nil); !errors.Is(err, ErrInvalidUpstream) {
		t.Fatalf("bad risk error = %v, want ErrInvalidUpstream", err)
	}
}

func TestRegistryValidationRejectsStrictHTTPRemote(t *testing.T) {
	upstream := validMCPUpstream("tenant-a", "remote-http")
	upstream.Endpoint = "http://203.0.113.10/mcp"
	err := Validate(context.Background(), &upstream, string(PolicyModeEnterpriseStrict), []string{"remote-http"})
	if !errors.Is(err, ErrUnsafeEndpoint) {
		t.Fatalf("strict HTTP remote error = %v, want ErrUnsafeEndpoint", err)
	}
}

func TestRegistryValidationRejectsUnsafeTenantID(t *testing.T) {
	upstream := validMCPUpstream("*:malicious", "tenant-escape")
	err := Validate(context.Background(), &upstream, string(PolicyModeObserve), nil)
	if !errors.Is(err, ErrInvalidUpstream) {
		t.Fatalf("unsafe tenant id error = %v, want ErrInvalidUpstream", err)
	}
}

func TestRegistryValidationRejectsLoopbackHostWithFragmentNoise(t *testing.T) {
	upstream := validMCPUpstream("tenant-a", "fragment-noise")
	upstream.Endpoint = "https://127.0.0.1:8443/mcp#@attacker.example"
	err := Validate(context.Background(), &upstream, string(PolicyModeEnterpriseStrict), []string{"fragment-noise"})
	if !errors.Is(err, ErrUnsafeEndpoint) {
		t.Fatalf("fragment-noise endpoint error = %v, want ErrUnsafeEndpoint", err)
	}
}
