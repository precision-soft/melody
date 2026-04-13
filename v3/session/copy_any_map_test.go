package session

import (
	"testing"
)

func TestCopyAnyMap_NilReturnsEmptyMap(t *testing.T) {
	result := copyAnyMap(nil)

	if nil == result {
		t.Fatalf("expected non-nil map for nil input")
	}

	if 0 != len(result) {
		t.Fatalf("expected empty map, got %d entries", len(result))
	}
}

func TestCopyAnyMap_ShallowCopyIsolatesChanges(t *testing.T) {
	original := map[string]any{
		"key": "value",
	}

	copied := copyAnyMap(original)

	copied["key"] = "changed"

	if "value" != original["key"].(string) {
		t.Fatalf("expected original to remain unchanged")
	}
}

func TestCopyAnyMap_DeepCopiesNestedMaps(t *testing.T) {
	nested := map[string]any{
		"nestedKey": "nestedValue",
	}

	original := map[string]any{
		"outer": nested,
	}

	copied := copyAnyMap(original)

	copiedNested, ok := copied["outer"].(map[string]any)
	if false == ok {
		t.Fatalf("expected nested map in copy")
	}

	copiedNested["nestedKey"] = "changed"

	if "nestedValue" != nested["nestedKey"].(string) {
		t.Fatalf("expected original nested map to remain unchanged")
	}
}

func TestCopyAnyMap_DeepCopiesDeeplyNestedMaps(t *testing.T) {
	level3 := map[string]any{
		"deep": "value",
	}

	level2 := map[string]any{
		"level3": level3,
	}

	level1 := map[string]any{
		"level2": level2,
	}

	copied := copyAnyMap(level1)

	copiedLevel3 := copied["level2"].(map[string]any)["level3"].(map[string]any)
	copiedLevel3["deep"] = "changed"

	if "value" != level3["deep"].(string) {
		t.Fatalf("expected deeply nested original to remain unchanged")
	}
}

func TestCopyAnyMap_PreservesNonMapValues(t *testing.T) {
	original := map[string]any{
		"stringValue": "hello",
		"intValue":    42,
		"boolValue":   true,
		"nilValue":    nil,
	}

	copied := copyAnyMap(original)

	if "hello" != copied["stringValue"].(string) {
		t.Fatalf("expected string value preserved")
	}

	if 42 != copied["intValue"].(int) {
		t.Fatalf("expected int value preserved")
	}

	if true != copied["boolValue"].(bool) {
		t.Fatalf("expected bool value preserved")
	}

	if nil != copied["nilValue"] {
		t.Fatalf("expected nil value preserved")
	}
}
