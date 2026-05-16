package edge

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

const secretRefPrefix = "secret://"

var (
	mcpUpstreamNameRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
	mcpUpstreamTenantRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
)

func Validate(ctx context.Context, upstream *UpstreamServer, policyMode string, allowlist []string) error {
	return ValidateMCPUpstream(ctx, upstream, policyMode, allowlist)
}

func ValidateMCPUpstream(ctx context.Context, upstream *UpstreamServer, policyMode string, allowlist []string) error {
	if upstream == nil {
		return ErrInvalidUpstream
	}
	name := strings.TrimSpace(upstream.Name)
	if !mcpUpstreamNameRE.MatchString(name) {
		return fmt.Errorf("%w: name", ErrInvalidUpstream)
	}
	tenantID := strings.TrimSpace(upstream.TenantID)
	if tenantID == "" || (tenantID != "*" && !mcpUpstreamTenantRE.MatchString(tenantID)) {
		return fmt.Errorf("%w: tenant_id", ErrInvalidUpstream)
	}
	if err := validateMCPUpstreamSecret(upstream.AuthSecretRef); err != nil {
		return err
	}
	if isMCPEnterpriseStrict(policyMode) && !mcpUpstreamAllowed(name, allowlist) {
		return ErrUpstreamNotAllowlisted
	}
	if err := validateMCPUpstreamRisk(upstream.Risk); err != nil {
		return err
	}
	switch strings.TrimSpace(upstream.Transport) {
	case "http", "sse":
		return validateMCPUpstreamURL(ctx, upstream.Endpoint, policyMode)
	case "stdio":
		return validateMCPUpstreamCommand(upstream.Command)
	default:
		return ErrInvalidTransport
	}
}

func validateMCPUpstreamSecret(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if !strings.HasPrefix(ref, secretRefPrefix) {
		return ErrSecretMustUseRef
	}
	return nil
}

func validateMCPUpstreamRisk(risk string) error {
	switch strings.TrimSpace(risk) {
	case "", "low", "medium", "high", "critical":
		return nil
	default:
		return fmt.Errorf("%w: risk", ErrInvalidUpstream)
	}
}

func validateMCPUpstreamURL(ctx context.Context, raw, policyMode string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w: endpoint", ErrUnsafeEndpoint)
	}
	strict := isMCPEnterpriseStrict(policyMode)
	host := strings.TrimSpace(u.Hostname())
	if strict {
		unsafe, err := mcpHostResolvesUnsafe(ctx, host)
		if err != nil || unsafe {
			return ErrUnsafeEndpoint
		}
	}
	if strict && u.Scheme != "https" {
		return ErrUnsafeEndpoint
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ErrUnsafeEndpoint
	}
	return nil
}

func mcpHostResolvesUnsafe(ctx context.Context, host string) (bool, error) {
	if host == "" {
		return true, nil
	}
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return mcpIPUnsafe(ip), nil
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return false, err
	}
	for _, ip := range ips {
		if mcpIPUnsafe(ip) {
			return true, nil
		}
	}
	return false, nil
}

func mcpIPUnsafe(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsUnspecified()
}

func validateMCPUpstreamCommand(command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("%w: command", ErrInvalidUpstream)
	}
	for _, part := range command {
		if strings.TrimSpace(part) == "" {
			return fmt.Errorf("%w: command", ErrInvalidUpstream)
		}
		if mcpCommandHasShellMetachar(part) {
			return ErrShellMetacharsRejected
		}
	}
	return nil
}

func mcpCommandHasShellMetachar(part string) bool {
	for _, token := range []string{";", "&&", "||", "|", ">", "<", "`", "$("} {
		if strings.Contains(part, token) {
			return true
		}
	}
	return false
}

func isMCPEnterpriseStrict(policyMode string) bool {
	return strings.EqualFold(strings.TrimSpace(policyMode), string(PolicyModeEnterpriseStrict))
}

func mcpUpstreamAllowed(name string, allowlist []string) bool {
	for _, candidate := range allowlist {
		if strings.TrimSpace(candidate) == name {
			return true
		}
	}
	return false
}
