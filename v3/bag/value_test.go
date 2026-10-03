package bag

import (
    "net/url"
    "testing"
)

func TestBagString_StringOrDefault_HasNonEmptyString(t *testing.T) {
    parameterBag := NewParameterBag()

    _, exists := String(parameterBag, "name")
    if true == exists {
        t.Fatalf("expected exists to be false")
    }

    if "default" != StringOrDefault(parameterBag, "name", "default") {
        t.Fatalf("expected default value")
    }

    parameterBag.Set("name", "   ")
    if false != HasNonEmptyString(parameterBag, "name") {
        t.Fatalf("expected HasNonEmptyString to be false for whitespace")
    }

    parameterBag.Set("name", "value")
    value, exists := String(parameterBag, "name")
    if false == exists {
        t.Fatalf("expected exists to be true")
    }
    if "value" != value {
        t.Fatalf("expected value %q, got %q", "value", value)
    }
}

func TestBagInt_ConversionsAndErrors(t *testing.T) {
    parameterBag := NewParameterBag()

    _, exists, err := Int(parameterBag, "n")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if true == exists {
        t.Fatalf("expected exists false")
    }

    parameterBag.Set("n", int64(10))
    value, exists, err := Int(parameterBag, "n")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 10 != value {
        t.Fatalf("expected %d, got %d", 10, value)
    }

    parameterBag.Set("n", " 42 ")
    value, exists, err = Int(parameterBag, "n")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 42 != value {
        t.Fatalf("expected %d, got %d", 42, value)
    }

    parameterBag.Set("n", "not-a-number")
    _, exists, err = Int(parameterBag, "n")
    if false == exists {
        t.Fatalf("expected exists true even when conversion fails")
    }
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestBagBool_ConversionsAndErrors(t *testing.T) {
    parameterBag := NewParameterBag()

    parameterBag.Set("b", true)
    value, exists, err := Bool(parameterBag, "b")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if true != value {
        t.Fatalf("expected true")
    }

    parameterBag.Set("b", " false ")
    value, exists, err = Bool(parameterBag, "b")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if false != value {
        t.Fatalf("expected false")
    }

    parameterBag.Set("b", " yes ")
    value, exists, err = Bool(parameterBag, "b")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if true != value {
        t.Fatalf("expected true")
    }

    parameterBag.Set("b", " off ")
    value, exists, err = Bool(parameterBag, "b")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if false != value {
        t.Fatalf("expected false")
    }

    parameterBag.Set("b", "not-bool")
    _, exists, err = Bool(parameterBag, "b")
    if false == exists {
        t.Fatalf("expected exists true even when conversion fails")
    }
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestBagFloat64_ConversionsAndErrors(t *testing.T) {
    parameterBag := NewParameterBag()

    parameterBag.Set("f", float32(1.5))
    value, exists, err := Float64(parameterBag, "f")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 1.5 != value {
        t.Fatalf("expected %v, got %v", 1.5, value)
    }

    parameterBag.Set("f", " 2.25 ")
    value, exists, err = Float64(parameterBag, "f")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 2.25 != value {
        t.Fatalf("expected %v, got %v", 2.25, value)
    }

    parameterBag.Set("f", "not-float")
    _, exists, err = Float64(parameterBag, "f")
    if false == exists {
        t.Fatalf("expected exists true even when conversion fails")
    }
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestValue_PresentNilReportsUnsetAcrossAllAccessors(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("key", nil)

    if false == parameterBag.Has("key") {
        t.Fatalf("expected the key to be present")
    }

    if _, exists := String(parameterBag, "key"); true == exists {
        t.Fatalf("expected String to report a nil value as unset")
    }

    if _, exists, _ := Int(parameterBag, "key"); true == exists {
        t.Fatalf("expected Int to report a nil value as unset")
    }

    if _, exists, _ := Bool(parameterBag, "key"); true == exists {
        t.Fatalf("expected Bool to report a nil value as unset")
    }

    if "fallback" != StringOrDefault(parameterBag, "key", "fallback") {
        t.Fatalf("expected the default to be used for a nil value")
    }

    parameterBag.Set("key", "value")

    if value, exists := String(parameterBag, "key"); false == exists || "value" != value {
        t.Fatalf("expected a set value to be reported, got %q exists=%v", value, exists)
    }
}

func TestString_ReadsTheFirstValueOfARepeatedKey(t *testing.T) {
    parameterBag := NewParameterBagFromValues(url.Values{"name": {"a", "b"}})

    value, exists := String(parameterBag, "name")
    if false == exists || "a" != value {
        t.Fatalf("expected the first value of the repeated key, got exists=%v value=%q", exists, value)
    }

    if "a" != StringOrDefault(parameterBag, "name", "anonymous") {
        t.Fatalf("expected StringOrDefault to deliver the first value, not the fallback")
    }

    if false == HasNonEmptyString(parameterBag, "name") {
        t.Fatalf("expected HasNonEmptyString to see the first value")
    }

    if slice, exists := StringSlice(parameterBag, "name"); false == exists || 2 != len(slice) {
        t.Fatalf("expected the whole list through StringSlice, got exists=%v slice=%v", exists, slice)
    }
}

func TestString_ReportsAnEmptyListAsUnset(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("tags", []string{})

    if value, exists := String(parameterBag, "tags"); true == exists || "" != value {
        t.Fatalf("expected an empty list to read as unset, got exists=%v value=%q", exists, value)
    }

    if "anonymous" != StringOrDefault(parameterBag, "tags", "anonymous") {
        t.Fatalf("expected the fallback for an empty list")
    }
}

/* a key that appeared once in url.Values is stored as the string it is, and only a genuinely repeated key stays a slice — the separation String and Input depend on. */
func TestNewParameterBagFromValues_KeepsTheSingleAndTheRepeatedKeyApartByType(t *testing.T) {
    parameterBag := NewParameterBagFromValues(url.Values{
        "single":   []string{"one"},
        "repeated": []string{"one", "two"},
        "empty":    []string{},
    })

    if value, exists := String(parameterBag, "single"); false == exists || "one" != value {
        t.Fatalf("expected the single occurrence as a string, got %q exists=%v", value, exists)
    }

    values, exists := StringSlice(parameterBag, "repeated")
    if false == exists || 2 != len(values) {
        t.Fatalf("expected the repeated key as a slice, got %#v exists=%v", values, exists)
    }

    if true == parameterBag.Has("empty") {
        t.Fatalf("expected a key with no values to stay out of the bag")
    }
}

func TestBagString_NonStringScalarReportsAbsentAndFallsBackToDefault(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("port", 9000)

    _, exists := String(parameterBag, "port")
    if true == exists {
        t.Fatalf("expected a non-string scalar to report absent, not present-but-empty")
    }

    if "8080" != StringOrDefault(parameterBag, "port", "8080") {
        t.Fatalf("expected StringOrDefault to substitute the default for a non-string value")
    }

    if true == HasNonEmptyString(parameterBag, "port") {
        t.Fatalf("expected HasNonEmptyString to report false for a non-string value")
    }
}
