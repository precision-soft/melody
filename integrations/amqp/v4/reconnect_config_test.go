package amqp

import (
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

func TestResolveReconnectOver_AnEmptyConfigOverTheDefaultIsEveryRuleDefault(t *testing.T) {
    resolved, resolveErr := resolveReconnectOver(defaultReconnect, ReconnectConfig{})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if time.Second != resolved.Initial || 30*time.Second != resolved.Max || 2.0 != resolved.Factor {
        t.Fatalf("expected 1s / 30s / 2, got %+v", resolved)
    }
}

func TestResolveReconnectOver_AFieldLeftOutIsTheParents(t *testing.T) {
    parent := bound.ResolvedBackoff{Initial: 2 * time.Second, Max: time.Minute, Factor: 4.0}

    resolved, resolveErr := resolveReconnectOver(parent, ReconnectConfig{Backoff: bound.Backoff{Max: bound.Of(10 * time.Second)}})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 2*time.Second != resolved.Initial {
        t.Fatalf("expected the inherited initial backoff 2s, got %s", resolved.Initial)
    }

    if 10*time.Second != resolved.Max {
        t.Fatalf("expected the overridden max backoff 10s, got %s", resolved.Max)
    }

    if 4.0 != resolved.Factor {
        t.Fatalf("expected the inherited factor 4, got %v", resolved.Factor)
    }
}

func TestResolveReconnectOver_EveryPresentFieldIsTheOverrides(t *testing.T) {
    parent := bound.ResolvedBackoff{Initial: 2 * time.Second, Max: time.Minute, Factor: 4.0}

    resolved, resolveErr := resolveReconnectOver(parent, ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(3 * time.Second), Max: bound.Of(5 * time.Second), Factor: 1.5}})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 3*time.Second != resolved.Initial || 5*time.Second != resolved.Max || 1.5 != resolved.Factor {
        t.Fatalf("expected 3s / 5s / 1.5, got %+v", resolved)
    }
}

func TestResolveReconnectOver_RefusesAValueOutsideItsRuleNamingTheField(t *testing.T) {
    cases := []struct {
        name   string
        config ReconnectConfig
        field  string
    }{
        {name: "initial zero", config: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Duration(0))}}, field: "ReconnectConfig.Backoff.Initial"},
        {name: "initial below the minimum", config: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Microsecond)}}, field: "ReconnectConfig.Backoff.Initial"},
        {name: "initial unlimited", config: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.UnlimitedDuration()}}, field: "ReconnectConfig.Backoff.Initial"},
        {name: "max zero", config: ReconnectConfig{Backoff: bound.Backoff{Max: bound.Of(time.Duration(0))}}, field: "ReconnectConfig.Backoff.Max"},
        {name: "max unlimited", config: ReconnectConfig{Backoff: bound.Backoff{Max: bound.UnlimitedDuration()}}, field: "ReconnectConfig.Backoff.Max"},
        {name: "initial above the default cap", config: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Minute)}}, field: "ReconnectConfig.Backoff.Initial"},
        {name: "factor below one", config: ReconnectConfig{Backoff: bound.Backoff{Factor: 0.5}}, field: "ReconnectConfig.Backoff.Factor"},
        {name: "factor negative", config: ReconnectConfig{Backoff: bound.Backoff{Factor: -2}}, field: "ReconnectConfig.Backoff.Factor"},
        {name: "factor NaN", config: ReconnectConfig{Backoff: bound.Backoff{Factor: math.NaN()}}, field: "ReconnectConfig.Backoff.Factor"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            _, resolveErr := resolveReconnectOver(defaultReconnect, testCase.config)
            expectErrorNamingField(t, resolveErr, testCase.field)
        })
    }
}

func TestResolveReconnectOver_ACapLoweredUnderAnInheritedInitialIsRefusedNamingBoth(t *testing.T) {
    parent := bound.ResolvedBackoff{Initial: time.Second, Max: 30 * time.Second, Factor: 2}

    _, resolveErr := resolveReconnectOver(parent, ReconnectConfig{Backoff: bound.Backoff{Max: bound.Of(500 * time.Millisecond)}})
    expectErrorNamingField(t, resolveErr, "ReconnectConfig.Backoff.Initial")

    errorContext := exception.LogContext(resolveErr, exception.LogContextConfig{})
    if "ReconnectConfig.Backoff.Max" != errorContext["cappedBy"] {
        t.Fatalf("expected the refusal to name the cap, got %v", errorContext)
    }

    if "1s" != errorContext["value"] || "500ms" != errorContext["maximum"] {
        t.Fatalf("expected the merged values in the refusal, got %v", errorContext)
    }
}

func TestResolveReconnectOver_AnInitialEqualToTheCapIsAdmitted(t *testing.T) {
    resolved, resolveErr := resolveReconnectOver(defaultReconnect, ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(30 * time.Second)}})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 30*time.Second != resolved.Initial {
        t.Fatalf("expected 30s, got %s", resolved.Initial)
    }
}

func TestResolveReconnectOver_AFactorOfExactlyOneIsAConstantBackoff(t *testing.T) {
    resolved, resolveErr := resolveReconnectOver(defaultReconnect, ReconnectConfig{Backoff: bound.Backoff{Factor: 1}})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 1.0 != resolved.Factor {
        t.Fatalf("expected factor 1, got %v", resolved.Factor)
    }

    if delay := resolved.Delay(5); time.Second != delay {
        t.Fatalf("expected a constant backoff of 1s, got %s", delay)
    }
}

func TestResolveReconnectOver_AnOverflowingFactorStaysClampedToTheCap(t *testing.T) {
    reconnect := bound.ResolvedBackoff{Initial: time.Second, Max: 30 * time.Second, Factor: 1e10}

    if delay := reconnect.Delay(3); 30*time.Second != delay {
        t.Fatalf("expected the backoff clamped to 30s, got %v", delay)
    }
}

func TestReconnectBackoffShouldReset_ComparesAgainstTheInitialBackoff(t *testing.T) {
    reconnect := bound.ResolvedBackoff{Initial: time.Second, Max: 30 * time.Second, Factor: 2}

    if true == reconnectBackoffShouldReset(reconnect, 0) {
        t.Fatalf("expected a zero-lived subscription not to reset the backoff")
    }

    if true == reconnectBackoffShouldReset(reconnect, time.Second-1) {
        t.Fatalf("expected a sub-threshold subscription not to reset the backoff")
    }

    if false == reconnectBackoffShouldReset(reconnect, time.Second) {
        t.Fatalf("expected a subscription that lived the initial backoff to reset")
    }
}
