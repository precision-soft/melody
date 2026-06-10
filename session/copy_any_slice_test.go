package session

import "testing"

func TestCopyAnyMap_DeepCopiesSlicesOfMaps(t *testing.T) {
    nested := map[string]any{"role": "admin"}
    original := map[string]any{"items": []any{nested}}

    copied := copyAnyMap(original)

    nested["role"] = "mutated"

    items, ok := copied["items"].([]any)
    if false == ok || 1 != len(items) {
        t.Fatalf("expected a copied []any of length 1, got %#v", copied["items"])
    }

    copiedNested, ok := items[0].(map[string]any)
    if false == ok {
        t.Fatalf("expected a map element inside the copied slice, got %#v", items[0])
    }

    if "admin" != copiedNested["role"] {
        t.Fatalf("mutating a map inside a []any after copy must not corrupt the copy, got %v", copiedNested["role"])
    }
}
