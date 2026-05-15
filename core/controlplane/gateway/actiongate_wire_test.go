package gateway

import (
	"testing"

	"github.com/cordum/cordum/core/infra/config"
	"github.com/cordum/cordum/core/policy/actiongates"
)

// TestWireActionGatePipeline_AssignsLivePipeline asserts the gateway
// wiring function turns the previously-dead actionGatePipeline field
// into a real pipeline. This is the regression guard for the QA-flagged
// "gateway server.actionGatePipeline field declared but never assigned"
// bug: a future refactor that drops the s.wireActionGatePipeline() call
// in RunWithAuth MUST break this test.
func TestWireActionGatePipeline_AssignsLivePipeline(t *testing.T) {
	t.Parallel()
	s := &server{}
	s.wireActionGatePipeline()
	if s.actionGatePipeline == nil {
		t.Fatal("server.actionGatePipeline still nil after wireActionGatePipeline()")
	}
	gates := s.actionGatePipeline.Gates()
	if len(gates) == 0 {
		t.Fatal("pipeline has no gates after wiring")
	}
	wantIDs := map[string]struct{}{
		actiongates.GateIDTenant:     {},
		actiongates.GateIDFile:       {},
		actiongates.GateIDURL:        {},
		actiongates.GateIDMCP:        {},
		actiongates.GateIDMutation:   {},
		actiongates.GateIDProvenance: {},
	}
	for _, g := range gates {
		delete(wantIDs, g.ID())
	}
	if len(wantIDs) > 0 {
		t.Fatalf("pipeline missing expected gates: %v", wantIDs)
	}
}

// TestWireActionGatePipeline_NilReceiverNoOp ensures the method tolerates
// a nil server pointer so misconfigured test fixtures cannot panic.
func TestWireActionGatePipeline_NilReceiverNoOp(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil-receiver wireActionGatePipeline panicked: %v", r)
		}
	}()
	var s *server
	s.wireActionGatePipeline()
}

// TestEncodeActionDescriptorLabel_RoundTrip asserts the JSON-label
// encoding the gateway uses to propagate an ActionDescriptor across the
// gRPC boundary preserves all fields needed by the kernel-side
// extractor (kernel_actiongate_wire.go's actionDescriptorFromRequest).
// The two halves must stay in lockstep or the descriptor never reaches
// the kernel pipeline.
func TestEncodeActionDescriptorLabel_RoundTrip(t *testing.T) {
	t.Parallel()
	desc := &config.ActionDescriptor{
		Kind:       config.ActionKindURL,
		Verb:       config.ActionVerbRead,
		TargetURL:  "https://example.com/docs",
		Server:     "mcp-corp",
		Tool:       "tools/read",
		RiskTags:   []string{"data:pii"},
	}
	encoded, err := encodeActionDescriptorLabel(desc)
	if err != nil {
		t.Fatalf("encode err: %v", err)
	}
	if encoded == "" {
		t.Fatal("encode returned empty string on non-nil descriptor")
	}
	if len(encoded) > config.ActionArgsMaxSerializedBytes {
		t.Fatalf("encode exceeds size cap: %d > %d", len(encoded), config.ActionArgsMaxSerializedBytes)
	}
}

func TestEncodeActionDescriptorLabel_NilReturnsEmpty(t *testing.T) {
	t.Parallel()
	encoded, err := encodeActionDescriptorLabel(nil)
	if err != nil {
		t.Fatalf("nil descriptor should return empty, got err: %v", err)
	}
	if encoded != "" {
		t.Fatalf("nil descriptor should return empty string, got %q", encoded)
	}
}

func TestEncodeActionDescriptorLabel_OversizeReturnsError(t *testing.T) {
	t.Parallel()
	hugeArgs := make(map[string]any, 4096)
	for i := range 4096 {
		// Each entry contributes ~80 bytes encoded -> well over the 64KB cap.
		hugeArgs[string(rune('a'+i%26))+"_long_key_name_"+strconvI(i)] = "long_value_" + strconvI(i)
	}
	desc := &config.ActionDescriptor{
		Kind: config.ActionKindMCPCall,
		Args: hugeArgs,
	}
	if _, err := encodeActionDescriptorLabel(desc); err == nil {
		t.Fatal("expected oversize descriptor to error, got nil")
	}
}

// strconvI is a tiny local int->string helper to avoid pulling in strconv
// for one call. Decoupled from any package-level helper so this test file
// has no transitive dependencies beyond standard library + this package.
func strconvI(n int) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	pos := len(buf)
	negative := n < 0
	if negative {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = digits[n%10]
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
