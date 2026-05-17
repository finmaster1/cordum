package k8s

import (
	"regexp"
	"strings"
)

// maxFieldBytes caps the size of every persisted string field this
// package writes. Longer inputs are truncated with a sentinel suffix
// per design doc §5.3.
const maxFieldBytes = 2048

// secretMarkerPatterns mirrors core/edge/shadow/redaction.go:23-36 so
// the K8s detector can run the EDGE-140 8-pattern strip at extraction
// time without depending on the unexported shadow.stripSecretMarkers
// helper. Keeping the two lists in sync is intentional: this package
// is the FIRST line of defense (extraction time) and the shadow store
// is the SECOND line (write time on EvidenceSummary). The duplication
// is documented in CHANGELOG.md so future patterns are added to both.
var secretMarkerPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z0-9 ]+PRIVATE KEY[-A-Z]*-----`),
	regexp.MustCompile(`-----BEGIN [A-Z0-9 ]+CERTIFICATE-----`),
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{16,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`gho_[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`xoxb-[A-Za-z0-9\-]{16,}`),
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9\-_\.]{8,}`),
}

// redactField is the universal entry point for every string the
// detector persists. It strips the 8 secret patterns and caps the
// output at maxFieldBytes. Detector code MUST funnel every
// caller-visible string through redactField — never through
// fmt.Sprintf directly — so a single contract change here propagates.
func redactField(s string) string {
	for _, re := range secretMarkerPatterns {
		s = re.ReplaceAllString(s, "<REDACTED>")
	}
	if len(s) > maxFieldBytes {
		s = s[:maxFieldBytes-len(" …truncated")] + " …truncated"
	}
	return s
}

// imageTagSafe returns the registry+name portion of a container image
// string with the tag scrubbed if it shows secret-shape. Plain semver
// tags pass through unchanged; anything matching secretMarkerPatterns
// degrades to "<image>:<redacted>".
func imageTagSafe(image string) string {
	if image == "" {
		return ""
	}
	colon := strings.LastIndex(image, ":")
	at := strings.Index(image, "@")
	if at > 0 {
		image = image[:at] // drop @sha256:... digest, not a leak but noisy
	}
	if colon <= 0 {
		return redactField(image)
	}
	base := image[:colon]
	tag := image[colon+1:]
	for _, re := range secretMarkerPatterns {
		if re.MatchString(tag) {
			return redactField(base) + ":<redacted>"
		}
	}
	return redactField(base) + ":" + redactField(tag)
}

// leadingToken returns the first space-and-shell-meta-stripped token
// of an arg or command slice entry. Per design doc §5.2 the detector
// captures ONLY this leading token — never the subsequent values.
func leadingToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	return s
}
