package messagebus

import (
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

func TestValidateHeaders_Rules(t *testing.T) {
    limits, limitsErr := ResolveHeaderLimits(HeaderLimitsConfig{MaxCount: bound.Of(3), MaxBytes: bound.Of(64)}, "TransportConfig.Headers")
    if nil != limitsErr {
        t.Fatalf("resolve: %v", limitsErr)
    }

    secret := "s3cr3t-value"

    for _, probe := range []struct {
        name    string
        headers map[string]string
        field   string
    }{
        {name: "admitted", headers: map[string]string{"request-id": "a", "Trace.Parent_1": "b", "9lives": ""}},
        {name: "none", headers: nil},
        {name: "empty name", headers: map[string]string{"": secret}, field: "TransportConfig.Headers"},
        {name: "leading dot", headers: map[string]string{".name": secret}, field: "TransportConfig.Headers"},
        {name: "leading dash", headers: map[string]string{"-name": secret}, field: "TransportConfig.Headers"},
        {name: "space", headers: map[string]string{"a b": secret}, field: "TransportConfig.Headers"},
        {name: "colon", headers: map[string]string{"a:b": secret}, field: "TransportConfig.Headers"},
        {name: "65 characters", headers: map[string]string{strings.Repeat("a", 65): secret}, field: "TransportConfig.Headers"},
        {name: "carriage return", headers: map[string]string{"name": secret + "\r"}, field: "TransportConfig.Headers"},
        {name: "line feed", headers: map[string]string{"name": secret + "\n"}, field: "TransportConfig.Headers"},
        {name: "nul", headers: map[string]string{"name": secret + "\x00"}, field: "TransportConfig.Headers"},
        {name: "invalid utf-8", headers: map[string]string{"name": secret + "\xff"}, field: "TransportConfig.Headers"},
        {name: "count", headers: map[string]string{"a": secret, "b": "", "c": "", "d": ""}, field: "TransportConfig.Headers.MaxCount"},
        {name: "bytes over names and values", headers: map[string]string{strings.Repeat("n", 40): secret + strings.Repeat("v", 13)}, field: "TransportConfig.Headers.MaxBytes"},
    } {
        validateErr := ValidateHeaders(probe.headers, limits)
        if "" == probe.field {
            if nil != validateErr {
                t.Fatalf("%s: expected the headers admitted, got %v", probe.name, validateErr)
            }

            continue
        }

        if false == errors.Is(validateErr, ErrHeadersRefused) {
            t.Fatalf("%s: expected a refusal caused by ErrHeadersRefused, got %v", probe.name, validateErr)
        }

        context := exception.LogContext(validateErr, exception.LogContextConfig{})
        if probe.field != context["field"] {
            t.Fatalf("%s: expected the refusal to name %s, got %v", probe.name, probe.field, context)
        }

        if true == strings.Contains(fmt.Sprint(context), secret) || true == strings.Contains(validateErr.Error(), secret) {
            t.Fatalf("%s: expected the refusal to carry no value, got %v", probe.name, context)
        }
    }

    if nil != ValidateHeaders(map[string]string{strings.Repeat("n", 32): strings.Repeat("v", 32)}, limits) {
        t.Fatalf("expected exactly MaxBytes to be admitted")
    }

    if nil != ValidateHeaders(map[string]string{strings.Repeat("a", 64): ""}, limits) {
        t.Fatalf("expected a 64-character name to be admitted")
    }
}

func TestValidateHeaders_TheZeroLimitsAreTheDefaults(t *testing.T) {
    headers := map[string]string{}
    for index := 0; index < DefaultHeaderMaxCount; index++ {
        headers[fmt.Sprintf("h%d", index)] = ""
    }

    if validateErr := ValidateHeaders(headers, HeaderLimits{}); nil != validateErr {
        t.Fatalf("expected %d headers admitted under the zero limits, got %v", DefaultHeaderMaxCount, validateErr)
    }

    headers["one-more"] = ""
    if false == errors.Is(ValidateHeaders(headers, HeaderLimits{}), ErrHeadersRefused) {
        t.Fatalf("expected one header above the default count refused")
    }

    if false == errors.Is(ValidateHeaders(map[string]string{"big": strings.Repeat("v", DefaultHeaderMaxBytes)}, HeaderLimits{}), ErrHeadersRefused) {
        t.Fatalf("expected headers above the default bytes refused")
    }
}

func TestValidateReceivedHeaders_AppliesTheRuleWithoutTheCountAndTheBytes(t *testing.T) {
    headers := map[string]string{"big": strings.Repeat("v", DefaultHeaderMaxBytes)}
    for index := 0; index <= DefaultHeaderMaxCount; index++ {
        headers[fmt.Sprintf("h%d", index)] = "value"
    }

    if validateErr := ValidateReceivedHeaders(headers, "TransportConfig.Headers"); nil != validateErr {
        t.Fatalf("expected headers over the count and the bytes admitted, got %v", validateErr)
    }

    for _, refused := range []map[string]string{{"a b": "value"}, {"": "value"}, {"name": "a\tb"}, {"name": "\xff"}} {
        validateErr := ValidateReceivedHeaders(refused, "TransportConfig.Headers")
        if false == errors.Is(validateErr, ErrHeadersRefused) {
            t.Fatalf("expected %q refused by the rule, got %v", refused, validateErr)
        }

        if "TransportConfig.Headers" != exception.LogContext(validateErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the refusal to name the field, got %v", exception.LogContext(validateErr, exception.LogContextConfig{}))
        }
    }
}

func TestIsWritableHeader_AnswersTheNameAndValueRule(t *testing.T) {
    for _, probe := range []struct {
        name     string
        value    string
        writable bool
    }{
        {name: "request-id", value: "request-7", writable: true},
        {name: strings.Repeat("a", 64), value: "", writable: true},
        {name: "", value: "value"},
        {name: "bad name", value: "value"},
        {name: strings.Repeat("a", 65), value: "value"},
        {name: "tabbed", value: "a\tb"},
        {name: "invalid", value: "\xff"},
    } {
        if probe.writable != IsWritableHeader(probe.name, probe.value) {
            t.Fatalf("expected IsWritableHeader(%q, %q) = %v", probe.name, probe.value, probe.writable)
        }
    }
}

func TestResolveHeaderLimits_RefusesUnlimitedAndValuesBelowTheMinimumNamingTheField(t *testing.T) {
    for _, probe := range []struct {
        config HeaderLimitsConfig
        field  string
    }{
        {config: HeaderLimitsConfig{MaxCount: bound.UnlimitedCount()}, field: "StoreConfig.Headers.MaxCount"},
        {config: HeaderLimitsConfig{MaxCount: bound.Of(0)}, field: "StoreConfig.Headers.MaxCount"},
        {config: HeaderLimitsConfig{MaxBytes: bound.UnlimitedCount()}, field: "StoreConfig.Headers.MaxBytes"},
        {config: HeaderLimitsConfig{MaxBytes: bound.Of(63)}, field: "StoreConfig.Headers.MaxBytes"},
    } {
        _, resolveErr := ResolveHeaderLimits(probe.config, "StoreConfig.Headers")
        if nil == resolveErr || probe.field != exception.LogContext(resolveErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected a refusal naming %s, got %v", probe.field, resolveErr)
        }
    }
}
