package edge

import (
	"strings"
	"testing"
)

func TestRegistryUpdateCreatesUniqueBackupsForRapidUpdates(t *testing.T) {
	ctx, registry, client := newMCPUpstreamRegistryForTest(t)
	upstream := validMCPUpstream("tenant-a", "rapid-update")
	upstream.Endpoint = "https://one.example/mcp"
	if err := registry.Create(ctx, &upstream); err != nil {
		t.Fatalf("Create: %v", err)
	}

	upstream.Endpoint = "https://two.example/mcp"
	if err := registry.Update(ctx, &upstream); err != nil {
		t.Fatalf("Update #1: %v", err)
	}
	upstream.Endpoint = "https://three.example/mcp"
	if err := registry.Update(ctx, &upstream); err != nil {
		t.Fatalf("Update #2: %v", err)
	}

	keys, err := client.Keys(ctx, "edge:mcp:upstream:bak:tenant-a:rapid-update:*").Result()
	if err != nil {
		t.Fatalf("Keys backup: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("backup keys = %v, want two unique backups", keys)
	}
	payloads := make([]string, 0, len(keys))
	for _, key := range keys {
		payload, err := client.Get(ctx, key).Result()
		if err != nil {
			t.Fatalf("Get backup %s: %v", key, err)
		}
		payloads = append(payloads, payload)
	}
	joined := strings.Join(payloads, "\n")
	if !strings.Contains(joined, "https://one.example/mcp") || !strings.Contains(joined, "https://two.example/mcp") {
		t.Fatalf("backup payloads missing prior versions: %s", joined)
	}
}
