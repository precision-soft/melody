package bag

import (
    "net/url"
    "strings"
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

/* @info a key set to nil is present but carries no value: String used to report it as set while Int, Bool, Float64 and Duration reported it as unset, so Has and the typed accessors contradicted each other on the same state */
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

/* the request bags keep the single and the repeated key apart by type, so a []string landing under String is a genuine array: the empty string would lose the value and one element would hide the rest, which is why the read refuses loudly toward StringSlice/StringAt instead of guessing. */
func TestString_RefusesASliceNamingTheParameter(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("tags", []string{"a", "b"})

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected reading a string slice as one string to panic")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError || false == strings.Contains(recoveredErr.Error(), "cannot be read as one string") {
            t.Fatalf("expected the refusal to name the shape, got %v", recoveredValue)
        }
    }()

    _, _ = String(parameterBag, "tags")
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
