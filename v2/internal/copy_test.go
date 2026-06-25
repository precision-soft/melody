package internal

import (
    "testing"
)

func TestCopyAnyMap_NilReturnsEmptyMap(t *testing.T) {
    result := CopyAnyMap(nil)

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

    copied := CopyAnyMap(original)

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

    copied := CopyAnyMap(original)

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

    copied := CopyAnyMap(level1)

    copiedLevel3 := copied["level2"].(map[string]any)["level3"].(map[string]any)
    copiedLevel3["deep"] = "changed"

    if "value" != level3["deep"].(string) {
        t.Fatalf("expected deeply nested original to remain unchanged")
    }
}

func TestCopyAnyMap_DeepCopiesSlicesContainingMaps(t *testing.T) {
    inner := map[string]any{"action": "read"}
    original := map[string]any{
        "permissions": []any{inner},
    }

    copied := CopyAnyMap(original)

    copiedSlice, ok := copied["permissions"].([]any)
    if false == ok || 1 != len(copiedSlice) {
        t.Fatalf("expected permissions slice in copy")
    }

    copiedSlice[0].(map[string]any)["action"] = "write"

    if "read" != inner["action"].(string) {
        t.Fatalf("mutating a map inside a copied slice leaked into the original")
    }
}

func TestCopyAnyMap_PreservesNonMapValues(t *testing.T) {
    original := map[string]any{
        "stringValue": "hello",
        "intValue":    42,
        "boolValue":   true,
        "nilValue":    nil,
    }

    copied := CopyAnyMap(original)

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

func TestCopyAnySlice_NilReturnsNil(t *testing.T) {
    result := CopyAnySlice(nil)

    if nil != result {
        t.Fatalf("expected nil for nil slice input")
    }
}

func TestCopyAnySlice_CopiesMapsInsideSlice(t *testing.T) {
    inner := map[string]any{"x": 1}
    original := []any{inner}

    copied := CopyAnySlice(original)

    copied[0].(map[string]any)["x"] = 99

    if 99 == inner["x"].(int) {
        t.Fatalf("mutating copied slice element leaked into original")
    }
}

func TestCopyAnyMap_DeepCopiesTypedSlices(t *testing.T) {
    original := map[string]any{
        "roles": []string{"user"},
    }

    copied := CopyAnyMap(original)

    copiedRoles, ok := copied["roles"].([]string)
    if false == ok {
        t.Fatalf("expected copied roles to remain a []string")
    }

    copiedRoles[0] = "admin"

    originalRoles := original["roles"].([]string)
    if "user" != originalRoles[0] {
        t.Fatalf("mutating the copy leaked into the original: got %q, want %q", originalRoles[0], "user")
    }
}

func TestCopyAnyMap_DeepCopiesTypedMaps(t *testing.T) {
    original := map[string]any{
        "flags": map[string]int{"a": 1},
    }

    copied := CopyAnyMap(original)

    copiedFlags := copied["flags"].(map[string]int)
    copiedFlags["a"] = 99

    originalFlags := original["flags"].(map[string]int)
    if 1 != originalFlags["a"] {
        t.Fatalf("mutating the copied typed map leaked into the original: got %d, want 1", originalFlags["a"])
    }
}

func TestCopyAnyMap_DeepCopiesTypedSliceOfTypedSlices(t *testing.T) {
    original := map[string]any{
        "matrix": [][]string{{"a"}},
    }

    copied := CopyAnyMap(original)

    copiedMatrix := copied["matrix"].([][]string)
    copiedMatrix[0][0] = "z"

    originalMatrix := original["matrix"].([][]string)
    if "a" != originalMatrix[0][0] {
        t.Fatalf("mutating the nested typed slice leaked into the original: got %q, want %q", originalMatrix[0][0], "a")
    }
}

func TestCopyAnyMap_CyclicValueDoesNotStackOverflow(t *testing.T) {
    /* @important a self-referential value reached through the deep copy must terminate via the depth bound rather than recurse until the goroutine stack overflows (a fatal error no recover() can catch); the test completing is the assertion. */
    cyclic := map[string]any{}
    cyclic["self"] = cyclic
    cyclic["name"] = "value"

    copied := CopyAnyMap(cyclic)

    if "value" != copied["name"].(string) {
        t.Fatalf("expected the non-cyclic entry to be deep-copied, got %v", copied["name"])
    }
    if nil == copied["self"] {
        t.Fatalf("expected the cyclic entry to be present in the copy")
    }
}

func TestCopyAnySlice_CyclicValueDoesNotStackOverflow(t *testing.T) {
    /* @important same bound for a self-referential slice reached through an interface element. */
    cyclic := make([]any, 2)
    cyclic[0] = cyclic
    cyclic[1] = "value"

    copied := CopyAnySlice(cyclic)

    if 2 != len(copied) {
        t.Fatalf("expected the cyclic slice to be copied with both elements, got %d", len(copied))
    }
    if "value" != copied[1].(string) {
        t.Fatalf("expected the non-cyclic element to be copied, got %v", copied[1])
    }
}
