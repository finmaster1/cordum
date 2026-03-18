package schema

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestValidateSchema(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	if err := ValidateSchema("test", schema, map[string]any{"name": "ok"}); err != nil {
		t.Fatalf("expected valid schema: %v", err)
	}
	if err := ValidateSchema("test", schema, map[string]any{"nope": "bad"}); err == nil {
		t.Fatalf("expected schema validation error")
	}
}

func TestValidateMapInline(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
		"required": []any{"id"},
	}
	if err := ValidateMap(schema, map[string]any{"id": "x"}); err != nil {
		t.Fatalf("expected valid schema: %v", err)
	}
	if err := ValidateMap(schema, map[string]any{}); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestNormalizeValue(t *testing.T) {
	data := json.RawMessage(`{"k":"v"}`)
	val, err := normalizeValue(data)
	if err != nil {
		t.Fatalf("normalize raw: %v", err)
	}
	m, ok := val.(map[string]any)
	if !ok || m["k"] != "v" {
		t.Fatalf("unexpected normalized value")
	}
	val, err = normalizeValue([]byte(`{"k":"v"}`))
	if err != nil {
		t.Fatalf("normalize bytes: %v", err)
	}
	if _, ok := val.(map[string]any); !ok {
		t.Fatalf("expected map from bytes")
	}
}

func TestValidateSchemaEmpty(t *testing.T) {
	if err := ValidateSchema("test", nil, nil); err == nil {
		t.Fatalf("expected error for empty schema")
	}
	if err := ValidateSchema("test", []byte{}, nil); err == nil {
		t.Fatalf("expected error for empty schema")
	}
}

func TestValidateMapEmpty(t *testing.T) {
	if err := ValidateMap(nil, nil); err == nil {
		t.Fatalf("expected error for empty map schema")
	}
	if err := ValidateMap(map[string]any{}, nil); err == nil {
		t.Fatalf("expected error for empty map schema")
	}
}

func TestNormalizeValueInvalidJSON(t *testing.T) {
	if _, err := normalizeValue(json.RawMessage("{")); err == nil {
		t.Fatalf("expected error for invalid raw json")
	}
	if _, err := normalizeValue([]byte("{")); err == nil {
		t.Fatalf("expected error for invalid byte json")
	}
}

func TestSchemaIDDefault(t *testing.T) {
	if got := schemaID(""); got != "inmemory://schema" {
		t.Fatalf("unexpected schema id: %s", got)
	}
}

func TestValidateSchemaWithResolver_CrossRef(t *testing.T) {
	// Schema A: defines an Address type
	schemaA := []byte(`{
		"$id": "https://cordum.io/schemas/test/address.json",
		"type": "object",
		"properties": {
			"street": {"type": "string"},
			"city": {"type": "string"}
		},
		"required": ["street", "city"]
	}`)

	// Schema B: references Schema A via $ref
	schemaB := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"address": {"$ref": "https://cordum.io/schemas/test/address.json"}
		},
		"required": ["name", "address"]
	}`)

	resolver := func(url string) (io.ReadCloser, error) {
		if url == "https://cordum.io/schemas/test/address.json" {
			return io.NopCloser(strings.NewReader(string(schemaA))), nil
		}
		return nil, fmt.Errorf("unknown schema: %s", url)
	}

	// Valid data
	valid := map[string]any{
		"name":    "Alice",
		"address": map[string]any{"street": "123 Main St", "city": "Springfield"},
	}
	if err := ValidateSchemaWithResolver("test-b", schemaB, valid, resolver); err != nil {
		t.Fatalf("expected valid: %v", err)
	}

	// Invalid: address missing required field
	invalid := map[string]any{
		"name":    "Bob",
		"address": map[string]any{"street": "456 Elm St"},
	}
	if err := ValidateSchemaWithResolver("test-b", schemaB, invalid, resolver); err == nil {
		t.Fatalf("expected validation error for missing city")
	}
}

func TestValidateSchemaWithResolver_UnknownRef(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"data": {"$ref": "https://cordum.io/schemas/unknown/nope.json"}
		}
	}`)

	resolver := func(url string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("not found: %s", url)
	}

	err := ValidateSchemaWithResolver("test", schema, map[string]any{"data": "x"}, resolver)
	if err == nil {
		t.Fatalf("expected compile error for unresolvable $ref")
	}
	if !strings.Contains(err.Error(), "compile schema") {
		t.Fatalf("expected compile error, got: %v", err)
	}
}

func TestValidateSchema_NilResolver_Regression(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	if err := ValidateSchemaWithResolver("test", schema, map[string]any{"name": "ok"}, nil); err != nil {
		t.Fatalf("expected valid with nil resolver: %v", err)
	}
	if err := ValidateSchemaWithResolver("test", schema, map[string]any{}, nil); err == nil {
		t.Fatalf("expected validation error with nil resolver")
	}
}
