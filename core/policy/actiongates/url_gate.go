package actiongates

import (
	"context"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cordum/cordum/core/infra/config"
	pb "github.com/cordum/cordum/core/protocol/pb/v1"
)

// URLGate blocks outbound URL access to cloud metadata services, known
// exfiltration destinations, DNS-rebinding hostnames that resolve to
// private/link-local IPs, and URLs whose query payload carries a recognized
// prompt-stash signature.
//
// Resolution uses an injectable HostResolver (default: net.DefaultResolver)
// so DNS rebinding tests are deterministic. A small LRU cache memoizes
// resolution results for 60s to keep per-request latency predictable.
type URLGate struct {
	resolver    HostResolver
	domainSeen  func(host string) bool
	resCacheMu  sync.Mutex
	resCache    map[string]resolverCacheEntry
	resCacheTTL time.Duration
	// resCacheMax bounds the resolver cache so attacker-influenced host
	// inputs cannot grow it without limit. Inserts past the cap evict
	// expired entries first, then drop the soonest-to-expire entry.
	resCacheMax int
}

type resolverCacheEntry struct {
	ips    []string
	err    error
	expiry time.Time
}

// defaultResolverCacheMax bounds the URL gate's resolver cache. Sized
// large enough to absorb realistic traffic spikes (a few hundred unique
// hosts per minute) while small enough to cap memory growth at a few
// hundred KiB even when every entry holds the maximum IP set.
const defaultResolverCacheMax = 4096

// URLGateOptions configures the URL gate. Resolver is required for DNS
// rebinding tests; DomainSeen is optional and feeds the REQUIRE_HUMAN
// path for PII POSTs to never-before-seen hosts (returns true for hosts
// already cached as approved). ResolverCacheMax bounds the resolver
// cache; zero or negative leaves the default in place.
type URLGateOptions struct {
	Resolver         HostResolver
	DomainSeen       func(host string) bool
	ResolverTTL      time.Duration
	ResolverCacheMax int
}

// NewURLGate constructs a URLGate. Resolver defaults to a net.LookupHost
// wrapper when unset.
func NewURLGate(opts URLGateOptions) *URLGate {
	resolver := opts.Resolver
	if resolver == nil {
		resolver = netResolver{}
	}
	ttl := opts.ResolverTTL
	if ttl == 0 {
		ttl = 60 * time.Second
	}
	maxEntries := opts.ResolverCacheMax
	if maxEntries <= 0 {
		maxEntries = defaultResolverCacheMax
	}
	return &URLGate{
		resolver:    resolver,
		domainSeen:  opts.DomainSeen,
		resCache:    make(map[string]resolverCacheEntry),
		resCacheTTL: ttl,
		resCacheMax: maxEntries,
	}
}

func (g *URLGate) ID() string { return GateIDURL }

type netResolver struct{}

func (netResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// metadataHosts: hostnames that always indicate a cloud metadata service.
var metadataHosts = map[string]string{
	"metadata.google.internal":        "gcp",
	"metadata.azure.com":              "azure",
	"169.254.169.254":                 "aws",
	"169.254.170.2":                   "aws_ecs",
	"100.100.100.200":                 "alibaba",
	"fd00:ec2::254":                   "aws_v6",
	"fd00:ec2:0:0:0:0:0:254":          "aws_v6",
	"fe80::a9fe:a9fe":                 "azure_link_local_v6",
	"fe80:0:0:0:0:0:a9fe:a9fe":        "azure_link_local_v6",
}

// exfilHostPatterns: substring + suffix match against the hostname. Suffixes
// MUST start with a dot or match the full host to avoid over-refusal
// (`example.ngrok.io.evil.com` should still be denied, while `ngrok.io.app`
// would only false-positive on exact `ngrok.io` ownership, which we accept).
var exfilHostPatterns = []struct {
	pattern   string
	subReason string
}{
	{"webhook.site", "exfil_host:webhook_site"},
	{".ngrok.io", "exfil_host:ngrok_io"},
	{".ngrok-free.app", "exfil_host:ngrok_free"},
	{".serveo.net", "exfil_host:serveo"},
	{".pipedream.net", "exfil_host:pipedream"},
	{".requestbin.com", "exfil_host:requestbin"},
	{".beeceptor.com", "exfil_host:beeceptor"},
	{".burpcollaborator.net", "exfil_host:burp"},
	{"burpcollaborator.net", "exfil_host:burp"},
	{"canarytokens.com", "exfil_host:canarytokens"},
	{".canarytokens.com", "exfil_host:canarytokens"},
	{".interactsh.com", "exfil_host:interactsh"},
}

// rebindDomains: well-known wildcard-IP DNS services. A hit means we MUST
// resolve and reject if the IP is private/link-local.
var rebindDomains = []string{".nip.io", ".sslip.io", ".xip.io"}

// pastePatterns: (host suffix, path prefix). Match when verb is a write
// (POST/PUT-style), since browsing a known paste is a read of public content.
var pastePatterns = []struct {
	hostSuffix string
	pathPrefix string
	subReason  string
}{
	{"pastebin.com", "/api/", "paste:pastebin_api"},
	{"gist.github.com", "/api/", "paste:gist_api"},
}

// promptStashKeys are JSON object keys whose presence inside a large query
// value strongly indicates prompt/context exfil.
var promptStashKeys = []string{
	`"messages"`, `"system"`, `"prompt"`, `"context_window"`, `"tools"`, `"conversation"`,
}

// promptExfilQueryLenThreshold is the per-query-value length (in bytes) above
// which we look for prompt stash keys. Small payloads pass through unchanged.
const promptExfilQueryLenThreshold = 1024

func (g *URLGate) Evaluate(ctx context.Context, in *config.PolicyInput) ActionGateDecision {
	if in == nil || in.Action == nil {
		return ActionGateDecision{}
	}
	act := in.Action
	if act.Kind != config.ActionKindURL {
		return ActionGateDecision{}
	}
	raw := strings.TrimSpace(act.TargetURL)
	if raw == "" {
		return ActionGateDecision{}
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ActionGateDecision{}
	}

	host := strings.ToLower(u.Hostname())
	port := u.Port()
	_ = port // reserved for future per-port rules

	// Strip IPv4-in-IPv6 prefix (e.g. ::ffff:169.254.169.254) when present so
	// the IP-class checks see the underlying v4 literal.
	host = unmapIPv4InIPv6(host)

	// userinfo-based bypass (http://google.com@169.254.169.254/): u.Host is
	// the authority post-userinfo, so this is already captured. We still flag
	// userinfo presence in audit so SIEM can review.
	hasUserinfo := u.User != nil

	// 1) Direct metadata-host hit by name or by literal IP.
	if sub, ok := metadataHosts[host]; ok {
		return g.deny(CodeAccessDenied, "metadata service access denied", "metadata_service:"+sub, raw, host, hasUserinfo)
	}

	// 2) Literal link-local / RFC1918 / loopback / multicast / unique-local.
	if ip := net.ParseIP(host); ip != nil {
		if class, ok := privateIPClass(ip); ok {
			subReason := "link_local"
			if class != "link_local" {
				subReason = class
			}
			return g.deny(CodeAccessDenied, "metadata service access denied", "metadata_service:"+subReason, raw, host, hasUserinfo)
		}
	}

	// 3) Known exfil hosts.
	if reason, ok := matchExfilHost(host); ok {
		return g.deny(CodeAccessDenied, "exfiltration destination denied", reason, raw, host, hasUserinfo)
	}

	// 4) Paste destinations on write verbs.
	if isWriteVerb(act.Verb) {
		if reason, ok := matchPaste(host, u.Path); ok {
			return g.deny(CodeAccessDenied, "paste destination denied", reason, raw, host, hasUserinfo)
		}
	}

	// 5) DNS rebinding via *.nip.io / sslip.io / xip.io: must resolve and
	// reject if any resolved IP is in a private class. Resolver errors
	// fail closed — a transient lookup failure on a wildcard-IP host is
	// the easiest way for an attacker to skip the rebind check, so we
	// refuse the request rather than treat the error as benign.
	if isRebindWildcardHost(host) {
		ips, _, err := g.resolve(ctx, host)
		if err != nil {
			return g.deny(CodeServiceUnavailable, "unable to validate destination host", "dns_rebind:resolver_error", raw, host, hasUserinfo)
		}
		for _, ip := range ips {
			if pip := net.ParseIP(ip); pip != nil {
				if class, ok := privateIPClass(pip); ok {
					return g.deny(CodeAccessDenied, "dns rebinding denied", "dns_rebind:"+class, raw, host, hasUserinfo)
				}
			}
		}
	}

	// 6) Prompt-exfil signature scan on query payload.
	if hasPromptExfilSignature(u) {
		return g.deny(CodeAccessDenied, "prompt context exfiltration denied", "prompt_exfil", raw, host, hasUserinfo)
	}

	// 7) PII POST to never-before-seen host -> REQUIRE_HUMAN.
	if isWriteVerb(act.Verb) && containsRiskTag(act.RiskTags, "data:pii") {
		seen := false
		if g.domainSeen != nil {
			seen = g.domainSeen(host)
		}
		if !seen {
			return ActionGateDecision{
				Decision:  pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN,
				GateID:    GateIDURL,
				Code:      CodeRequireHuman,
				Reason:    "human approval required for PII upload to new destination",
				SubReason: "pii_post:new_host",
				Extra: map[string]string{
					"gate":       GateIDURL,
					"sub_reason": "pii_post:new_host",
					"host":       host,
				},
			}
		}
	}

	return ActionGateDecision{Decision: pb.DecisionType_DECISION_TYPE_ALLOW, GateID: GateIDURL}
}

func (g *URLGate) deny(code, reason, subReason, raw, host string, hasUserinfo bool) ActionGateDecision {
	extra := map[string]string{
		"gate":       GateIDURL,
		"sub_reason": subReason,
		"host":       host,
		"raw_len":    itoa(len(raw)),
	}
	if hasUserinfo {
		extra["userinfo_present"] = "true"
	}
	return ActionGateDecision{
		Decision:  pb.DecisionType_DECISION_TYPE_DENY,
		GateID:    GateIDURL,
		Code:      code,
		Reason:    reason,
		SubReason: subReason,
		Extra:     extra,
	}
}

func (g *URLGate) resolve(ctx context.Context, host string) ([]string, bool, error) {
	g.resCacheMu.Lock()
	if ent, ok := g.resCache[host]; ok && time.Now().Before(ent.expiry) {
		g.resCacheMu.Unlock()
		return ent.ips, true, ent.err
	}
	g.resCacheMu.Unlock()

	ips, err := g.resolver.LookupHost(ctx, host)
	g.resCacheMu.Lock()
	g.evictIfFullLocked(host)
	g.resCache[host] = resolverCacheEntry{ips: ips, err: err, expiry: time.Now().Add(g.resCacheTTL)}
	g.resCacheMu.Unlock()
	return ips, false, err
}

// evictIfFullLocked enforces the resCacheMax bound. Caller must hold
// resCacheMu. The two-pass strategy is intentionally simple: sweep
// expired entries first (cheap; the common case once TTLs roll over),
// and only if still over capacity drop the entry with the soonest
// expiry. This caps worst-case memory regardless of attacker-driven
// host cardinality without dragging in a full LRU.
func (g *URLGate) evictIfFullLocked(replacingHost string) {
	if g.resCacheMax <= 0 {
		return
	}
	if _, replacing := g.resCache[replacingHost]; replacing {
		// We're overwriting an existing entry; size doesn't grow.
		return
	}
	if len(g.resCache) < g.resCacheMax {
		return
	}
	now := time.Now()
	for k, ent := range g.resCache {
		if !now.Before(ent.expiry) {
			delete(g.resCache, k)
		}
	}
	if len(g.resCache) < g.resCacheMax {
		return
	}
	var oldestKey string
	var oldestExpiry time.Time
	first := true
	for k, ent := range g.resCache {
		if first || ent.expiry.Before(oldestExpiry) {
			oldestKey = k
			oldestExpiry = ent.expiry
			first = false
		}
	}
	if oldestKey != "" {
		delete(g.resCache, oldestKey)
	}
}

// unmapIPv4InIPv6 normalizes ::ffff:1.2.3.4 (IPv4-mapped IPv6) and the brackets
// around v6 hostnames into a literal v4 string when applicable. `host` is
// already lowercased and bracketed v6 had its brackets stripped by url.Hostname.
func unmapIPv4InIPv6(host string) string {
	if !strings.Contains(host, ":") {
		return host
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return host
}

func privateIPClass(ip net.IP) (string, bool) {
	if ip == nil {
		return "", false
	}
	if ip.IsLoopback() {
		return "loopback", true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return "link_local", true
	}
	if ip.IsPrivate() {
		return "rfc1918", true
	}
	if ip.IsUnspecified() {
		return "unspecified", true
	}
	// Unique local (fc00::/7).
	if v6 := ip.To16(); v6 != nil && ip.To4() == nil {
		if v6[0]&0xfe == 0xfc {
			return "unique_local", true
		}
	}
	return "", false
}

func matchExfilHost(host string) (string, bool) {
	for _, p := range exfilHostPatterns {
		if p.pattern == host {
			return p.subReason, true
		}
		if strings.HasPrefix(p.pattern, ".") && strings.HasSuffix(host, p.pattern) {
			return p.subReason, true
		}
	}
	return "", false
}

func isRebindWildcardHost(host string) bool {
	for _, d := range rebindDomains {
		if strings.HasSuffix(host, d) {
			return true
		}
	}
	return false
}

func matchPaste(host, path string) (string, bool) {
	for _, p := range pastePatterns {
		if host == p.hostSuffix && strings.HasPrefix(path, p.pathPrefix) {
			return p.subReason, true
		}
	}
	return "", false
}

func hasPromptExfilSignature(u *url.URL) bool {
	if u.RawQuery == "" {
		return false
	}
	q := u.Query()
	for _, vals := range q {
		for _, v := range vals {
			if len(v) < promptExfilQueryLenThreshold {
				continue
			}
			if !looksLikeJSONWithStashKey(v) {
				continue
			}
			return true
		}
	}
	return false
}

func looksLikeJSONWithStashKey(s string) bool {
	// Cheap heuristic: starts/contains a JSON object and has at least one stash key.
	if !strings.ContainsAny(s, "{[") {
		return false
	}
	for _, k := range promptStashKeys {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func containsRiskTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}
