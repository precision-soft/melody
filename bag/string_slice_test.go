package bag

import (
    "testing"

    bagcontract "github.com/precision-soft/melody/bag/contract"
)

func TestStringSlice_CopiesSlice_SupportsSingleString_AndHandlesInvalidType(t *testing.T) {
    parameterBag := NewParameterBag()

    parameterBag.Set("tags", []string{"a", "b"})
    values, exists := StringSlice(parameterBag, "tags")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 2 != len(values) {
        t.Fatalf("expected 2 values")
    }

    values[0] = "changed"

    valuesAgain, exists := StringSlice(parameterBag, "tags")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if "a" != valuesAgain[0] {
        t.Fatalf("expected deep copy")
    }

    parameterBag.Set("one", "x")
    values, exists = StringSlice(parameterBag, "one")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 1 != len(values) {
        t.Fatalf("expected 1 value")
    }
    if "x" != values[0] {
        t.Fatalf("expected %q", "x")
    }

    parameterBag.Set("bad", 123)
    values, exists = StringSlice(parameterBag, "bad")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if nil != values {
        t.Fatalf("expected nil values for invalid type")
    }
}

func TestStringAt_ErrorsAndHappyPath(t *testing.T) {
    parameterBag := NewParameterBag()

    _, exists, err := StringAt(parameterBag, "tags", 0)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected exists false")
    }

    parameterBag.Set("tags", []string{"a", "b"})

    _, exists, err = StringAt(parameterBag, "tags", -1)
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if nil == err {
        t.Fatalf("expected error")
    }

    _, exists, err = StringAt(parameterBag, "tags", 2)
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if nil == err {
        t.Fatalf("expected error")
    }

    value, exists, err := StringAt(parameterBag, "tags", 1)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if "b" != value {
        t.Fatalf("expected %q, got %q", "b", value)
    }
}

func TestAppendString_AndAppendStringSlice(t *testing.T) {
    parameterBag := NewParameterBag()

    err := AppendString(parameterBag, "tags", "a")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    err = AppendString(parameterBag, "tags", "b")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    valuesAny, exists := parameterBag.Get("tags")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    values := valuesAny.([]string)
    if 2 != len(values) {
        t.Fatalf("expected 2 values")
    }

    parameterBag.Set("single", "x")
    err = AppendString(parameterBag, "single", "y")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    valuesAny, exists = parameterBag.Get("single")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    values = valuesAny.([]string)
    if 2 != len(values) {
        t.Fatalf("expected 2 values")
    }
    if "x" != values[0] {
        t.Fatalf("expected first value to be %q", "x")
    }
    if "y" != values[1] {
        t.Fatalf("expected second value to be %q", "y")
    }

    parameterBag.Set("bad", 123)
    err = AppendStringSlice(parameterBag, "bad", []string{"a", "b"})
    if nil == err {
        t.Fatalf("expected error")
    }

    valueAny, exists := parameterBag.Get("bad")
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 123 != valueAny.(int) {
        t.Fatalf("expected value to remain unchanged on error")
    }
}

func TestStringSlice_PresentNilReportsUnset(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("tags", nil)

    if false == parameterBag.Has("tags") {
        t.Fatalf("expected the key to be present")
    }

    values, exists := StringSlice(parameterBag, "tags")
    if true == exists {
        t.Fatalf("expected StringSlice to report a nil value as unset")
    }
    if nil != values {
        t.Fatalf("expected no values, got %v", values)
    }

    if _, exists, _ := StringAt(parameterBag, "tags", 0); true == exists {
        t.Fatalf("expected StringAt to report a nil value as unset")
    }
}

/* contractOnlyParameterBag implements the contract without being the concrete bag, which is what routes AppendString through its fallback: the fallback is the path a foreign implementation takes, and it is the one that cannot append inside a single critical section */
type contractOnlyParameterBag struct {
    values map[string]any
}

func newContractOnlyParameterBag() *contractOnlyParameterBag {
    return &contractOnlyParameterBag{values: make(map[string]any)}
}

func (instance *contractOnlyParameterBag) Set(name string, value any) {
    instance.values[name] = value
}

func (instance *contractOnlyParameterBag) Get(name string) (any, bool) {
    value, exists := instance.values[name]

    return value, exists
}

func (instance *contractOnlyParameterBag) Has(name string) bool {
    _, exists := instance.values[name]

    return exists
}

func (instance *contractOnlyParameterBag) Remove(name string) {
    delete(instance.values, name)
}

func (instance *contractOnlyParameterBag) Count() int {
    return len(instance.values)
}

func (instance *contractOnlyParameterBag) All() map[string]any {
    copied := make(map[string]any, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied
}

var _ bagcontract.ParameterBag = (*contractOnlyParameterBag)(nil)

/* @info a bag that is not melody's own takes the contract fallback, which has to answer the same way the concrete one does on every shape: an absent name, a name holding nil, a slice, a single string, and a value that is neither — the last refused by naming the type it found, because appending a string to it would mean deciding what the stored value meant */
func TestAppendString_ContractFallbackCoversEveryStoredShape(t *testing.T) {
    parameterBag := newContractOnlyParameterBag()

    if appendErr := AppendString(parameterBag, "absent", "first"); nil != appendErr {
        t.Fatalf("unexpected error appending to an absent name: %v", appendErr)
    }

    parameterBag.Set("nilValue", nil)
    if appendErr := AppendString(parameterBag, "nilValue", "first"); nil != appendErr {
        t.Fatalf("unexpected error appending over a nil value: %v", appendErr)
    }

    parameterBag.Set("single", "first")
    if appendErr := AppendString(parameterBag, "single", "second"); nil != appendErr {
        t.Fatalf("unexpected error appending to a single string: %v", appendErr)
    }

    if appendErr := AppendString(parameterBag, "absent", "second"); nil != appendErr {
        t.Fatalf("unexpected error appending to a slice: %v", appendErr)
    }

    for name, expected := range map[string][]string{
        "absent":   {"first", "second"},
        "nilValue": {"first"},
        "single":   {"first", "second"},
    } {
        stored, _ := parameterBag.Get(name)
        values, isSlice := stored.([]string)
        if false == isSlice {
            t.Fatalf("expected %q to hold a string slice, got %T", name, stored)
        }
        if len(expected) != len(values) {
            t.Fatalf("expected %d values under %q, got %#v", len(expected), name, values)
        }
        for index := range expected {
            if expected[index] != values[index] {
                t.Fatalf("expected %q under %q at %d, got %q", expected[index], name, index, values[index])
            }
        }
    }

    parameterBag.Set("number", 12)
    appendErr := AppendString(parameterBag, "number", "first")
    if nil == appendErr {
        t.Fatalf("expected appending to a value that is neither a string nor a slice to be refused")
    }
    if "parameter is not a string or string slice" != appendErr.Error() {
        t.Fatalf("unexpected refusal: %q", appendErr.Error())
    }
}

/* @info appending a whole list keeps every value and keeps their order, which is what a repeated query parameter is: the sibling test above pins the refusal, this one pins that the ordinary path actually lands all of them */
func TestAppendStringSlice_KeepsEveryValueInOrder(t *testing.T) {
    parameterBag := NewParameterBag()

    if appendErr := AppendStringSlice(parameterBag, "tags", []string{"a", "b", "c"}); nil != appendErr {
        t.Fatalf("unexpected error: %v", appendErr)
    }

    values, exists := StringSlice(parameterBag, "tags")
    if false == exists {
        t.Fatalf("expected the appended name to exist")
    }

    if 3 != len(values) || "a" != values[0] || "b" != values[1] || "c" != values[2] {
        t.Fatalf("expected every value in order, got %#v", values)
    }
}
