package rueidis

import (
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestResolveReconnect_AnAbsentConfigIsTheDefaultBackoff(t *testing.T) {
    resolved, resolveErr := resolveReconnect(ReconnectConfig{})
    if nil != resolveErr {
        t.Fatalf("expected the zero config to resolve, got %v", resolveErr)
    }

    expected := resolvedReconnect{initialBackoff: time.Second, maxBackoff: 30 * time.Second, backoffFactor: 2.0}
    if expected != resolved {
        t.Fatalf("expected %+v, got %+v", expected, resolved)
    }
}

func TestResolveReconnect_AGivenFieldWinsAndTheOthersTakeTheDefault(t *testing.T) {
    resolved, resolveErr := resolveReconnect(ReconnectConfig{MaxBackoff: bound.Of(10 * time.Second)})
    if nil != resolveErr {
        t.Fatalf("expected the config to resolve, got %v", resolveErr)
    }

    expected := resolvedReconnect{initialBackoff: time.Second, maxBackoff: 10 * time.Second, backoffFactor: 2.0}
    if expected != resolved {
        t.Fatalf("expected %+v, got %+v", expected, resolved)
    }
}

func TestResolveReconnect_AdmitsTheBoundaries(t *testing.T) {
    resolved, resolveErr := resolveReconnect(ReconnectConfig{
        InitialBackoff: bound.Of(time.Millisecond),
        MaxBackoff:     bound.Of(time.Millisecond),
        BackoffFactor:  1,
    })
    if nil != resolveErr {
        t.Fatalf("expected 1ms, an initial equal to the cap and the factor 1 to be admitted, got %v", resolveErr)
    }

    expected := resolvedReconnect{initialBackoff: time.Millisecond, maxBackoff: time.Millisecond, backoffFactor: 1}
    if expected != resolved {
        t.Fatalf("expected %+v, got %+v", expected, resolved)
    }
}

func TestResolveReconnect_RefusesAValueOutsideItsRuleNamingTheField(t *testing.T) {
    cases := map[string]struct {
        config ReconnectConfig
        field  string
    }{
        "unlimited initial backoff":       {config: ReconnectConfig{InitialBackoff: bound.UnlimitedDuration()}, field: "ReconnectConfig.InitialBackoff"},
        "sub-millisecond initial backoff": {config: ReconnectConfig{InitialBackoff: bound.Of(time.Microsecond)}, field: "ReconnectConfig.InitialBackoff"},
        "unlimited max backoff":           {config: ReconnectConfig{MaxBackoff: bound.UnlimitedDuration()}, field: "ReconnectConfig.MaxBackoff"},
        "sub-millisecond max backoff":     {config: ReconnectConfig{MaxBackoff: bound.Of(time.Microsecond)}, field: "ReconnectConfig.MaxBackoff"},
        "initial above the default cap":   {config: ReconnectConfig{InitialBackoff: bound.Of(2 * time.Minute)}, field: "ReconnectConfig.InitialBackoff"},
        "initial above a given cap":       {config: ReconnectConfig{InitialBackoff: bound.Of(5 * time.Second), MaxBackoff: bound.Of(4 * time.Second)}, field: "ReconnectConfig.InitialBackoff"},
        "factor 0.5":                      {config: ReconnectConfig{BackoffFactor: 0.5}, field: "ReconnectConfig.BackoffFactor"},
        "negative factor":                 {config: ReconnectConfig{BackoffFactor: -1}, field: "ReconnectConfig.BackoffFactor"},
        "NaN factor":                      {config: ReconnectConfig{BackoffFactor: math.NaN()}, field: "ReconnectConfig.BackoffFactor"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            _, resolveErr := resolveReconnect(testCase.config)
            expectErrorNamingField(t, resolveErr, testCase.field)
        })
    }
}

func TestResolveReconnect_TheCapRefusalNamesTheCappingField(t *testing.T) {
    _, resolveErr := resolveReconnect(ReconnectConfig{InitialBackoff: bound.Of(2 * time.Minute)})

    context := exceptionContextOf(resolveErr)
    if "ReconnectConfig.MaxBackoff" != context["cappedBy"] || "30s" != context["maximum"] {
        t.Fatalf("expected the refusal to name the cap and its 30s value, got %v", context)
    }
}
