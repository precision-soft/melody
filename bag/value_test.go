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

func TestString_PanicsOnAStringSlice(t *testing.T) {
    parameterBag := NewParameterBag()
    parameterBag.Set("repeated", []string{"1", "2"})

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the string slice to be refused with a panic")
        }
    }()

    _, _ = String(parameterBag, "repeated")
}

func TestString_DeliversTheSingleOccurrence(t *testing.T) {
    parameterBag := NewParameterBagFromValues(url.Values{"term": {"melody"}})

    value, exists := String(parameterBag, "term")
    if false == exists || "melody" != value {
        t.Fatalf("expected the single occurrence back, got exists=%v value=%q", exists, value)
    }

    if "melody" != StringOrDefault(parameterBag, "term", "fallback") {
        t.Fatalf("expected StringOrDefault to deliver the value, not the fallback")
    }

    if false == HasNonEmptyString(parameterBag, "term") {
        t.Fatalf("expected HasNonEmptyString to see the provided value")
    }
}

func TestBagTypedReaders_AnAbsentNameIsUnsetRatherThanTheZeroValue(t *testing.T) {
    parameterBag := NewParameterBag()

    boolValue, boolExists, boolErr := Bool(parameterBag, "missing")
    if true == boolExists || nil != boolErr || false != boolValue {
        t.Fatalf("expected Bool to report an absent name unset, got value=%v exists=%v err=%v", boolValue, boolExists, boolErr)
    }

    floatValue, floatExists, floatErr := Float64(parameterBag, "missing")
    if true == floatExists || nil != floatErr || 0 != floatValue {
        t.Fatalf("expected Float64 to report an absent name unset, got value=%v exists=%v err=%v", floatValue, floatExists, floatErr)
    }

    intValue, intExists, intErr := Int(parameterBag, "missing")
    if true == intExists || nil != intErr || 0 != intValue {
        t.Fatalf("expected Int to report an absent name unset, got value=%v exists=%v err=%v", intValue, intExists, intErr)
    }
}
