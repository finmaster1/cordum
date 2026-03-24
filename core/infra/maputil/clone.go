// Package maputil provides shared map utility functions used across
// the scheduler, workflow engine, and MCP server.
package maputil

// CloneStringMap returns a shallow copy of the map. Returns nil if the
// input is nil, preserving the nil vs empty-map distinction.
func CloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// CloneAnyMap returns a shallow copy of the map. Returns nil if the
// input is nil. Values are NOT deep-copied; use DeepCloneAnyMap if
// nested maps or slices must be isolated from the original.
func CloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// DeepCloneAnyMap returns a deep copy of the map, recursively cloning
// nested map[string]any values and []any slices. Primitive types (int,
// float64, string, bool) are copied by value. Unlike JSON round-trip,
// this preserves Go types — int stays int, not float64.
func DeepCloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCloneValue(v)
	}
	return out
}

func deepCloneValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return DeepCloneAnyMap(val)
	case []any:
		cp := make([]any, len(val))
		for i, elem := range val {
			cp[i] = deepCloneValue(elem)
		}
		return cp
	default:
		return v
	}
}
