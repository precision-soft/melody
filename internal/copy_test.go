package internal

import (
    "testing"
    "time"
)

/* the two defined types below are the only shapes that reach the reflect paths carrying interface elements: the type switch above them matches []any and map[string]any exactly, so a named type with the same underlying type walks past it, and an interface element holding nothing is the untyped nil the zero-value clauses exist for */
type copyTestAnySlice []any

type copyTestAnyMap map[string]any

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
    /* the test completing is the assertion: a stack overflow is a fatal error no recover() can catch, so the depth bound cannot be observed any other way */
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
    /* the test completing is the assertion here too */
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

func TestCopyAnySlice_SharedSubstructureCompletes(t *testing.T) {
    leaf := "x"
    node := []any{leaf}
    for i := 0; i < 28; i++ {
        node = []any{node, node}
    }

    done := make(chan struct{})
    go func() {
        _ = CopyAnySlice(node)
        close(done)
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatalf("expected the copy of a 28-level shared-substructure value to complete")
    }
}

func TestCopyAnyMap_CycleClosesOnTheCopy(t *testing.T) {
    cyclic := map[string]any{}
    cyclic["self"] = cyclic

    copied := CopyAnyMap(cyclic)

    innerValue, exists := copied["self"]
    if false == exists {
        t.Fatalf("expected the cyclic entry to be present in the copy")
    }

    innerMap, isMap := innerValue.(map[string]any)
    if false == isMap {
        t.Fatalf("expected the cyclic entry to remain a map, got %T", innerValue)
    }

    innerMap["probe"] = "written through the copy"
    if _, leaked := cyclic["probe"]; true == leaked {
        t.Fatalf("expected the cycle to close on the copy, not on the live original")
    }
    if _, present := copied["probe"]; false == present {
        t.Fatalf("expected the cyclic entry to be the copied map itself")
    }
}

func TestCopyAnyMap_PreservesSharing(t *testing.T) {
    shared := map[string]any{"key": "value"}
    original := map[string]any{
        "first":  shared,
        "second": shared,
    }

    copied := CopyAnyMap(original)

    firstCopy := copied["first"].(map[string]any)
    secondCopy := copied["second"].(map[string]any)

    firstCopy["probe"] = "written"
    if _, sharedInCopy := secondCopy["probe"]; false == sharedInCopy {
        t.Fatalf("expected both edges to reach the same copied node")
    }
    if _, leaked := shared["probe"]; true == leaked {
        t.Fatalf("expected the write through the copy to stay out of the original")
    }
}

func TestCopyAnyMap_PreservesSharingOfTypedMaps(t *testing.T) {
    shared := map[string]int{"count": 1}
    original := map[string]any{
        "first":  shared,
        "second": shared,
    }

    copied := CopyAnyMap(original)

    firstCopy := copied["first"].(map[string]int)
    secondCopy := copied["second"].(map[string]int)

    firstCopy["probe"] = 2
    if _, sharedInCopy := secondCopy["probe"]; false == sharedInCopy {
        t.Fatalf("expected both edges to reach the same copied typed map")
    }
    if _, leaked := shared["probe"]; true == leaked {
        t.Fatalf("expected the write through the copy to stay out of the original")
    }
}

func TestCopyAnyMap_PreservesSharingOfTypedSlices(t *testing.T) {
    shared := []string{"a"}
    original := map[string]any{
        "first":  shared,
        "second": shared,
    }

    copied := CopyAnyMap(original)

    firstCopy := copied["first"].([]string)
    secondCopy := copied["second"].([]string)

    firstCopy[0] = "written"
    if "written" != secondCopy[0] {
        t.Fatalf("expected both edges to reach the same copied typed slice")
    }
    if "a" != shared[0] {
        t.Fatalf("expected the write through the copy to stay out of the original")
    }
}

func TestCopyStringMap_NilInputAnswersAWritableEmptyMap(t *testing.T) {
    copied := CopyStringMap[string](nil)

    if nil == copied {
        t.Fatalf("expected a non-nil map for a nil input")
    }
    if 0 != len(copied) {
        t.Fatalf("expected an empty map, got %d entries", len(copied))
    }

    copied["key"] = "value"
    if "value" != copied["key"] {
        t.Fatalf("expected the answered map to be writable")
    }
}

func TestCopyAnySlice_ZeroLengthSliceDoesNotShareTheCallersCapacity(t *testing.T) {
    backing := make([]any, 1, 4)
    backing[0] = "kept"

    copied := CopyAnySlice(backing[:0])

    if nil == copied {
        t.Fatalf("expected a non-nil slice for a zero-length input")
    }
    if 0 != len(copied) {
        t.Fatalf("expected an empty slice, got %d elements", len(copied))
    }

    copied = append(copied, "written through the copy")

    if "kept" != backing[0].(string) {
        t.Fatalf("appending through the copy wrote into the caller's backing array: got %v", backing[0])
    }
}

func TestCopyAnyMap_AtTheDepthBoundTheValueIsAliased(t *testing.T) {
    const chainLength = maxCopyDepth + 2

    deepest := map[string]any{"marker": "original"}

    current := deepest
    for index := 0; index < chainLength; index++ {
        current = map[string]any{"next": current}
    }

    copied := CopyAnyMap(current)

    node := copied
    for index := 0; index < chainLength; index++ {
        next, isMap := node["next"].(map[string]any)
        if false == isMap {
            t.Fatalf("expected the chain to stay a chain of maps at level %d, got %T", index, node["next"])
        }
        node = next
    }

    node["probe"] = "written through the copy"

    if _, aliased := deepest["probe"]; false == aliased {
        t.Fatalf("expected the node past the depth bound to be returned as-is, not copied")
    }
}

func TestCopyAnyMap_TypedNilSliceStaysNil(t *testing.T) {
    original := map[string]any{
        "roles": []string(nil),
    }

    copied := CopyAnyMap(original)

    copiedRoles, isSlice := copied["roles"].([]string)
    if false == isSlice {
        t.Fatalf("expected the entry to remain a []string, got %T", copied["roles"])
    }
    if nil != copiedRoles {
        t.Fatalf("expected the typed nil slice to stay nil, got %#v", copiedRoles)
    }
}

func TestCopyAnyMap_TypedNilMapStaysNil(t *testing.T) {
    original := map[string]any{
        "flags": map[string]int(nil),
    }

    copied := CopyAnyMap(original)

    copiedFlags, isMap := copied["flags"].(map[string]int)
    if false == isMap {
        t.Fatalf("expected the entry to remain a map[string]int, got %T", copied["flags"])
    }
    if nil != copiedFlags {
        t.Fatalf("expected the typed nil map to stay nil, got %#v", copiedFlags)
    }
}

func TestCopyAnyMap_NilElementOfANamedSliceIsCopiedAsTheZeroValue(t *testing.T) {
    original := map[string]any{
        "list": copyTestAnySlice{nil, "value"},
    }

    copied := CopyAnyMap(original)

    copiedList, isSlice := copied["list"].(copyTestAnySlice)
    if false == isSlice {
        t.Fatalf("expected the entry to remain a copyTestAnySlice, got %T", copied["list"])
    }
    if 2 != len(copiedList) {
        t.Fatalf("expected both elements to be copied, got %d", len(copiedList))
    }
    if nil != copiedList[0] {
        t.Fatalf("expected the nil element to stay nil, got %#v", copiedList[0])
    }
    if "value" != copiedList[1].(string) {
        t.Fatalf("expected the element beside the nil one to be copied, got %#v", copiedList[1])
    }
}

func TestCopyAnyMap_NilValueOfANamedMapIsCopiedAsTheZeroValue(t *testing.T) {
    original := map[string]any{
        "document": copyTestAnyMap{"absent": nil, "present": "value"},
    }

    copied := CopyAnyMap(original)

    copiedDocument, isMap := copied["document"].(copyTestAnyMap)
    if false == isMap {
        t.Fatalf("expected the entry to remain a copyTestAnyMap, got %T", copied["document"])
    }
    if 2 != len(copiedDocument) {
        t.Fatalf("expected both entries to be copied, got %d", len(copiedDocument))
    }

    absentValue, absentExists := copiedDocument["absent"]
    if false == absentExists {
        t.Fatalf("expected the nil entry to be present in the copy")
    }
    if nil != absentValue {
        t.Fatalf("expected the nil entry to stay nil, got %#v", absentValue)
    }
    if "value" != copiedDocument["present"].(string) {
        t.Fatalf("expected the entry beside the nil one to be copied, got %#v", copiedDocument["present"])
    }
}

func TestCopyAnyMap_SubslicesOfOneArrayStayDistinct(t *testing.T) {
    backing := []string{"a", "b", "c"}
    original := map[string]any{
        "short": backing[:1],
        "long":  backing[:3],
    }

    copied := CopyAnyMap(original)

    shortCopy := copied["short"].([]string)
    longCopy := copied["long"].([]string)
    if 1 != len(shortCopy) || 3 != len(longCopy) {
        t.Fatalf("expected the two subslices to keep their lengths, got %d and %d", len(shortCopy), len(longCopy))
    }
}
