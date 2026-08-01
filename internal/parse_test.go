package internal

import (
    "math"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/exception"
)

/* the framework renders an error's message alone through Error(); the reason for a refusal travels as the cause, so the tests read it there */
func parseTestCauseMessage(t *testing.T, err error) string {
    t.Helper()

    exceptionErr, isException := err.(*exception.Error)
    if false == isException {
        t.Fatalf("expected an exception error, got %T", err)
    }

    causeErr := exceptionErr.Unwrap()
    if nil == causeErr {
        t.Fatalf("expected the refusal to carry a cause, got none on %q", err.Error())
    }

    return causeErr.Error()
}

/* @info a bare integer carries no unit and used to be read as nanoseconds — the interpretation almost no caller means — while the same value spelled as a string was refused for missing its unit; both spellings are refused now */
func TestDuration_RefusesBareInt(t *testing.T) {
    for _, value := range []any{30, int64(30)} {
        _, isSet, err := Duration(value, "probe")
        if false == isSet {
            t.Fatalf("expected the bare integer %#v to be reported as set", value)
        }
        if nil == err {
            t.Fatalf("expected the bare integer %#v to be refused", value)
        }
        if false == strings.Contains(parseTestCauseMessage(t, err), "carries no unit") {
            t.Fatalf("expected the refusal to name the missing unit, got cause %q", parseTestCauseMessage(t, err))
        }
    }
}

func TestDuration_AcceptsTypedDurationAndUnitString(t *testing.T) {
    typedValue, isSet, err := Duration(30*time.Second, "probe")
    if nil != err || false == isSet || 30*time.Second != typedValue {
        t.Fatalf("expected a typed duration to pass through, got %v/%v/%v", typedValue, isSet, err)
    }

    stringValue, isSet, err := Duration(" 45s ", "probe")
    if nil != err || false == isSet || 45*time.Second != stringValue {
        t.Fatalf("expected a unit string to parse, got %v/%v/%v", stringValue, isSet, err)
    }
}

func TestDuration_NilIsAbsent(t *testing.T) {
    _, isSet, err := Duration(nil, "probe")
    if nil != err || true == isSet {
        t.Fatalf("expected nil to be absent without error, got isSet=%v err=%v", isSet, err)
    }
}

func TestDuration_RefusesUnparseableString(t *testing.T) {
    _, isSet, err := Duration("5x", "probe")
    if nil == err || false == isSet {
        t.Fatalf("expected an unparseable string to be refused, got isSet=%v err=%v", isSet, err)
    }
}

/* @info strconv.ParseFloat parses "NaN", "Inf" and "Infinity" without an error, and a NaN that slips through disarms every ordered comparison downstream; the typed float branches carry the same refusal */
func TestFloat64_RefusesNonFinite(t *testing.T) {
    nonFiniteValues := []any{
        "NaN", "nan", "Inf", "+Inf", "-Inf", "Infinity",
        math.NaN(), math.Inf(1), math.Inf(-1),
        float32(float64(math.Inf(1))),
    }

    for _, value := range nonFiniteValues {
        _, isSet, err := Float64(value, "probe")
        if nil == err {
            t.Fatalf("expected the non-finite %#v to be refused (isSet=%v)", value, isSet)
        }
        if false == strings.Contains(parseTestCauseMessage(t, err), "not finite") {
            t.Fatalf("expected the refusal of %#v to name finiteness, got cause %q", value, parseTestCauseMessage(t, err))
        }
    }
}

func TestFloat64_AcceptsFiniteShapes(t *testing.T) {
    for _, probe := range []struct {
        value    any
        expected float64
    }{
        {value: 1.5, expected: 1.5},
        {value: float32(2), expected: 2},
        {value: 7, expected: 7},
        {value: int64(9), expected: 9},
        {value: " 3.25 ", expected: 3.25},
    } {
        parsedValue, isSet, err := Float64(probe.value, "probe")
        if nil != err || false == isSet || probe.expected != parsedValue {
            t.Fatalf("expected %#v to parse to %v, got %v/%v/%v", probe.value, probe.expected, parsedValue, isSet, err)
        }
    }
}

/* @info the three float64 refusals of Int carry three distinct causes — collapsed into one "is not a 'int'" they blamed the type, which the same branch accepts for 2.0 */
func TestInt_Float64RefusalsAreDistinct(t *testing.T) {
    for _, probe := range []struct {
        value    float64
        fragment string
    }{
        {value: math.NaN(), fragment: "not finite"},
        {value: math.Inf(1), fragment: "not finite"},
        {value: 2.5, fragment: "not an integral number"},
        {value: 1e300, fragment: "outside the int64 range"},
    } {
        _, isSet, err := Int(probe.value, "probe")
        if nil == err || false == isSet {
            t.Fatalf("expected %v to be refused, got isSet=%v err=%v", probe.value, isSet, err)
        }
        if false == strings.Contains(parseTestCauseMessage(t, err), probe.fragment) {
            t.Fatalf("expected the refusal of %v to carry %q, got cause %q", probe.value, probe.fragment, parseTestCauseMessage(t, err))
        }
    }
}

func TestInt_AcceptsExactBoundsAndStrings(t *testing.T) {
    boundValue, isSet, err := Int(-9223372036854775808.0, "probe")
    if nil != err || false == isSet || math.MinInt64 != boundValue {
        t.Fatalf("expected the exact lower bound to convert, got %v/%v/%v", boundValue, isSet, err)
    }

    _, _, err = Int(9223372036854775808.0, "probe")
    if nil == err {
        t.Fatalf("expected 2^63 to be refused")
    }

    stringValue, isSet, err := Int(" 123 ", "probe")
    if nil != err || false == isSet || 123 != stringValue {
        t.Fatalf("expected a trimmed base-10 string to parse, got %v/%v/%v", stringValue, isSet, err)
    }

    _, _, err = Int("0x10", "probe")
    if nil == err {
        t.Fatalf("expected a non-base-10 spelling to be refused")
    }
}

/* @info a typed-nil map is the absence it reads as: reported present it came back as an empty non-nil map and a caller branching on the presence flag silently lost its default — the strict accessors beside this one already answer absent for nil */
func TestMapStringString_TypedNilIsAbsent(t *testing.T) {
    var typedNil map[string]string
    value, isSet, err := MapStringString(typedNil, "probe")
    if nil != err || true == isSet || nil != value {
        t.Fatalf("expected a typed-nil map to be absent, got %#v/%v/%v", value, isSet, err)
    }
}

func TestMapStringString_CopiesThePresentMap(t *testing.T) {
    original := map[string]string{"key": "value"}
    copied, isSet, err := MapStringString(original, "probe")
    if nil != err || false == isSet {
        t.Fatalf("expected the map to be present, got isSet=%v err=%v", isSet, err)
    }

    copied["key"] = "changed"
    if "value" != original["key"] {
        t.Fatalf("expected the returned map to be a copy")
    }
}

func TestBool_ParsesTheDocumentedSpellings(t *testing.T) {
    trueValue, isSet, err := Bool("Yes", "probe")
    if nil != err || false == isSet || true != trueValue {
        t.Fatalf("expected \"Yes\" to parse true, got %v/%v/%v", trueValue, isSet, err)
    }

    _, _, err = Bool("maybe", "probe")
    if nil == err {
        t.Fatalf("expected an unknown spelling to be refused")
    }
}
