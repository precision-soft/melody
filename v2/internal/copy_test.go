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

type copyTestRecord struct {
    Name     string
    Tags     []string
    Counters map[string]int
}

type copyTestScalarRecord struct {
    Name  string
    Count int
}

type copyTestHiddenRecord struct {
    tags []string
}

type copyTestNode struct {
    Name string
    Next *copyTestNode
}

func TestCopyAnyMap_DeepCopiesPointerValues(t *testing.T) {
    original := 1
    source := map[string]any{
        "pointer": &original,
    }

    copied := CopyAnyMap(source)

    copiedPointer, ok := copied["pointer"].(*int)
    if false == ok {
        t.Fatalf("expected the copy to keep the pointer type, got %T", copied["pointer"])
    }

    *copiedPointer = 99

    if 1 != original {
        t.Fatalf("writing through the copied pointer reached the original: got %d, want 1", original)
    }
}

func TestCopyAnyMap_PreservesNilPointer(t *testing.T) {
    var absent *copyTestRecord

    source := map[string]any{
        "pointer": absent,
    }

    copied := CopyAnyMap(source)

    copiedPointer, ok := copied["pointer"].(*copyTestRecord)
    if false == ok {
        t.Fatalf("expected the copy to keep the pointer type, got %T", copied["pointer"])
    }

    if nil != copiedPointer {
        t.Fatalf("expected a nil pointer to stay nil, got %v", copiedPointer)
    }
}

func TestCopyAnyMap_DeepCopiesStructSliceField(t *testing.T) {
    record := copyTestRecord{
        Name: "record",
        Tags: []string{"first"},
    }

    source := map[string]any{
        "record": record,
    }

    copied := CopyAnyMap(source)

    copiedRecord, ok := copied["record"].(copyTestRecord)
    if false == ok {
        t.Fatalf("expected the copy to keep the struct type, got %T", copied["record"])
    }

    copiedRecord.Tags[0] = "changed"

    if "first" != record.Tags[0] {
        t.Fatalf("mutating the copied struct slice field reached the original: got %q, want %q", record.Tags[0], "first")
    }
}

func TestCopyAnyMap_DeepCopiesStructMapField(t *testing.T) {
    record := copyTestRecord{
        Name:     "record",
        Counters: map[string]int{"reads": 1},
    }

    source := map[string]any{
        "record": record,
    }

    copied := CopyAnyMap(source)

    copiedRecord := copied["record"].(copyTestRecord)
    copiedRecord.Counters["reads"] = 99

    if 1 != record.Counters["reads"] {
        t.Fatalf("mutating the copied struct map field reached the original: got %d, want 1", record.Counters["reads"])
    }
}

func TestCopyAnyMap_DeepCopiesPointerToStructWithReferenceField(t *testing.T) {
    record := &copyTestRecord{
        Name: "record",
        Tags: []string{"first"},
    }

    source := map[string]any{
        "record": record,
    }

    copied := CopyAnyMap(source)

    copiedRecord := copied["record"].(*copyTestRecord)
    copiedRecord.Name = "changed"
    copiedRecord.Tags[0] = "changed"

    if "record" != record.Name {
        t.Fatalf("writing through the copied pointer reached the original: got %q, want %q", record.Name, "record")
    }
    if "first" != record.Tags[0] {
        t.Fatalf("mutating the slice behind the copied pointer reached the original: got %q, want %q", record.Tags[0], "first")
    }
}

func TestCopyAnyMap_DeepCopiesStructInsideTypedSlice(t *testing.T) {
    records := []copyTestRecord{
        {
            Name: "record",
            Tags: []string{"first"},
        },
    }

    source := map[string]any{
        "records": records,
    }

    copied := CopyAnyMap(source)

    copiedRecords := copied["records"].([]copyTestRecord)
    copiedRecords[0].Tags[0] = "changed"

    if "first" != records[0].Tags[0] {
        t.Fatalf("mutating a struct inside the copied slice reached the original: got %q, want %q", records[0].Tags[0], "first")
    }
}

func TestCopyAnyMap_DeepCopiesArrayElements(t *testing.T) {
    original := [2][]string{
        {"first"},
        {"second"},
    }

    source := map[string]any{
        "matrix": original,
    }

    copied := CopyAnyMap(source)

    copiedMatrix := copied["matrix"].([2][]string)
    copiedMatrix[0][0] = "changed"

    if "first" != original[0][0] {
        t.Fatalf("mutating the copied array element reached the original: got %q, want %q", original[0][0], "first")
    }
}

func TestCopyAnyMap_ReturnsScalarStructUnchanged(t *testing.T) {
    record := copyTestScalarRecord{
        Name:  "record",
        Count: 3,
    }

    copied := CopyAnyMap(map[string]any{"record": record})

    copiedRecord, ok := copied["record"].(copyTestScalarRecord)
    if false == ok {
        t.Fatalf("expected the copy to keep the struct type, got %T", copied["record"])
    }

    if record != copiedRecord {
        t.Fatalf("expected the scalar struct to survive the copy unchanged, got %+v", copiedRecord)
    }
}

func TestCopyAnyMap_LeavesUnexportedStructFieldsShallow(t *testing.T) {
    /* reflection cannot write an unexported field without unsafe, so the whole-struct assignment is as deep as the copy reaches there. The contract is stated by this test rather than assumed, because the handle types depend on it: duplicating what an unexported field points at would hand back a value that no longer names the file, connection or location it stood for. */
    record := copyTestHiddenRecord{
        tags: []string{"first"},
    }

    copied := CopyAnyMap(map[string]any{"record": record})

    copiedRecord := copied["record"].(copyTestHiddenRecord)
    copiedRecord.tags[0] = "changed"

    if "changed" != record.tags[0] {
        t.Fatalf("expected the unexported field to stay shared, got %q", record.tags[0])
    }
}

func TestCopyAnyMap_PointerCycleDoesNotStackOverflow(t *testing.T) {
    /* the depth bound has to hold for a cycle closed through pointers as well, because each level of one costs more frames than a map level does: the pointer branch and the struct branch both recurse. The test completing is the assertion. */
    cyclic := &copyTestNode{
        Name: "node",
    }
    cyclic.Next = cyclic

    copied := CopyAnyMap(map[string]any{"node": cyclic})

    copiedNode, ok := copied["node"].(*copyTestNode)
    if false == ok {
        t.Fatalf("expected the copy to keep the pointer type, got %T", copied["node"])
    }

    if "node" != copiedNode.Name {
        t.Fatalf("expected the copied node to carry its name, got %q", copiedNode.Name)
    }
    if cyclic == copiedNode {
        t.Fatalf("expected the copied node to be a distinct allocation")
    }
}

func newBenchmarkTokenAttributes() map[string]any {
    return map[string]any{
        "tenant":      "acme",
        "plan":        "enterprise",
        "seatCount":   float64(42),
        "trial":       false,
        "issuer":      "https://login.example.com/",
        "permissions": []any{"order.read", "order.write", "invoice.read"},
        "profile": map[string]any{
            "locale":   "ro-RO",
            "timezone": "Europe/Bucharest",
        },
    }
}

func newBenchmarkSessionPayload() map[string]any {
    items := make([]any, 0, 20)
    for index := 0; index < 20; index++ {
        items = append(items, map[string]any{
            "sku":      "sku-00000",
            "quantity": float64(index),
            "price":    float64(index) * 1.5,
            "options":  []any{"gift", "express"},
        })
    }

    return map[string]any{
        "userIdentifier": "user-1234",
        "csrfToken":      "b2f1c0d4e5a6978877665544332211000fedcba9",
        "locale":         "ro-RO",
        "flash":          []any{"saved", "welcome back"},
        "cart": map[string]any{
            "currency": "RON",
            "total":    float64(1234.56),
            "items":    items,
        },
    }
}

func newBenchmarkStructPayload() map[string]any {
    return map[string]any{
        "record": copyTestRecord{
            Name:     "record",
            Tags:     []string{"first", "second", "third"},
            Counters: map[string]int{"reads": 1, "writes": 2},
        },
        "pointer": &copyTestRecord{
            Name: "pointed",
            Tags: []string{"first"},
        },
        "scalar": copyTestScalarRecord{
            Name:  "scalar",
            Count: 7,
        },
    }
}

func BenchmarkCopyAnyMap_TokenAttributes(b *testing.B) {
    source := newBenchmarkTokenAttributes()

    b.ReportAllocs()
    b.ResetTimer()

    for index := 0; index < b.N; index++ {
        _ = CopyAnyMap(source)
    }
}

func BenchmarkCopyAnyMap_SessionPayload(b *testing.B) {
    source := newBenchmarkSessionPayload()

    b.ReportAllocs()
    b.ResetTimer()

    for index := 0; index < b.N; index++ {
        _ = CopyAnyMap(source)
    }
}

func BenchmarkCopyAnyMap_StructPayload(b *testing.B) {
    source := newBenchmarkStructPayload()

    b.ReportAllocs()
    b.ResetTimer()

    for index := 0; index < b.N; index++ {
        _ = CopyAnyMap(source)
    }
}
