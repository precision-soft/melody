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

    if time.Second != resolved.initialBackoff || 30*time.Second != resolved.maxBackoff || 2.0 != resolved.backoffFactor {
        t.Fatalf("expected 1s / 30s / 2, got %+v", resolved)
    }
}

func TestResolveReconnectOver_AFieldLeftOutIsTheParents(t *testing.T) {
    parent := resolvedReconnect{initialBackoff: 2 * time.Second, maxBackoff: time.Minute, backoffFactor: 4.0}

    resolved, resolveErr := resolveReconnectOver(parent, ReconnectConfig{MaxBackoff: bound.Of(10 * time.Second)})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 2*time.Second != resolved.initialBackoff {
        t.Fatalf("expected the inherited initial backoff 2s, got %s", resolved.initialBackoff)
    }

    if 10*time.Second != resolved.maxBackoff {
        t.Fatalf("expected the overridden max backoff 10s, got %s", resolved.maxBackoff)
    }

    if 4.0 != resolved.backoffFactor {
        t.Fatalf("expected the inherited factor 4, got %v", resolved.backoffFactor)
    }
}

func TestResolveReconnectOver_EveryPresentFieldIsTheOverrides(t *testing.T) {
    parent := resolvedReconnect{initialBackoff: 2 * time.Second, maxBackoff: time.Minute, backoffFactor: 4.0}

    resolved, resolveErr := resolveReconnectOver(parent, ReconnectConfig{InitialBackoff: bound.Of(3 * time.Second), MaxBackoff: bound.Of(5 * time.Second), BackoffFactor: 1.5})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 3*time.Second != resolved.initialBackoff || 5*time.Second != resolved.maxBackoff || 1.5 != resolved.backoffFactor {
        t.Fatalf("expected 3s / 5s / 1.5, got %+v", resolved)
    }
}

func TestResolveReconnectOver_RefusesAValueOutsideItsRuleNamingTheField(t *testing.T) {
    cases := []struct {
        name   string
        config ReconnectConfig
        field  string
    }{
        {name: "initial zero", config: ReconnectConfig{InitialBackoff: bound.Of(time.Duration(0))}, field: "ReconnectConfig.InitialBackoff"},
        {name: "initial below the minimum", config: ReconnectConfig{InitialBackoff: bound.Of(time.Microsecond)}, field: "ReconnectConfig.InitialBackoff"},
        {name: "initial unlimited", config: ReconnectConfig{InitialBackoff: bound.UnlimitedDuration()}, field: "ReconnectConfig.InitialBackoff"},
        {name: "max zero", config: ReconnectConfig{MaxBackoff: bound.Of(time.Duration(0))}, field: "ReconnectConfig.MaxBackoff"},
        {name: "max unlimited", config: ReconnectConfig{MaxBackoff: bound.UnlimitedDuration()}, field: "ReconnectConfig.MaxBackoff"},
        {name: "initial above the default cap", config: ReconnectConfig{InitialBackoff: bound.Of(2 * time.Minute)}, field: "ReconnectConfig.InitialBackoff"},
        {name: "factor below one", config: ReconnectConfig{BackoffFactor: 0.5}, field: "ReconnectConfig.BackoffFactor"},
        {name: "factor negative", config: ReconnectConfig{BackoffFactor: -2}, field: "ReconnectConfig.BackoffFactor"},
        {name: "factor NaN", config: ReconnectConfig{BackoffFactor: math.NaN()}, field: "ReconnectConfig.BackoffFactor"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            _, resolveErr := resolveReconnectOver(defaultReconnect, testCase.config)
            expectErrorNamingField(t, resolveErr, testCase.field)
        })
    }
}

func TestResolveReconnectOver_ACapLoweredUnderAnInheritedInitialIsRefusedNamingBoth(t *testing.T) {
    parent := resolvedReconnect{initialBackoff: time.Second, maxBackoff: 30 * time.Second, backoffFactor: 2}

    _, resolveErr := resolveReconnectOver(parent, ReconnectConfig{MaxBackoff: bound.Of(500 * time.Millisecond)})
    expectErrorNamingField(t, resolveErr, "ReconnectConfig.InitialBackoff")

    errorContext := exception.LogContext(resolveErr, exception.LogContextConfig{})
    if "ReconnectConfig.MaxBackoff" != errorContext["cappedBy"] {
        t.Fatalf("expected the refusal to name the cap, got %v", errorContext)
    }

    if "1s" != errorContext["value"] || "500ms" != errorContext["maximum"] {
        t.Fatalf("expected the merged values in the refusal, got %v", errorContext)
    }
}

func TestResolveReconnectOver_AnInitialEqualToTheCapIsAdmitted(t *testing.T) {
    resolved, resolveErr := resolveReconnectOver(defaultReconnect, ReconnectConfig{InitialBackoff: bound.Of(30 * time.Second)})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 30*time.Second != resolved.initialBackoff {
        t.Fatalf("expected 30s, got %s", resolved.initialBackoff)
    }
}

func TestResolveReconnectOver_AFactorOfExactlyOneIsAConstantBackoff(t *testing.T) {
    resolved, resolveErr := resolveReconnectOver(defaultReconnect, ReconnectConfig{BackoffFactor: 1})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 1.0 != resolved.backoffFactor {
        t.Fatalf("expected factor 1, got %v", resolved.backoffFactor)
    }

    if next := nextReconnectBackoff(resolved, time.Second); time.Second != next {
        t.Fatalf("expected a constant backoff of 1s, got %s", next)
    }
}

func TestNextReconnectBackoff_ClampsOverflowingProduct(t *testing.T) {
    /* a BackoffFactor large enough to push the float product past the int64 nanosecond range would wrap the time.Duration conversion negative, skip the cap, and feed time.After a no-delay backoff; the result must stay clamped to the cap */
    reconnect := resolvedReconnect{initialBackoff: time.Second, maxBackoff: 30 * time.Second, backoffFactor: 1e10}

    next := nextReconnectBackoff(reconnect, time.Second)

    if 30*time.Second != next {
        t.Fatalf("expected the backoff clamped to 30s, got %v", next)
    }
}

func TestNextReconnectBackoff_GrowsByTheFactorUnderTheCap(t *testing.T) {
    reconnect := resolvedReconnect{initialBackoff: time.Second, maxBackoff: 30 * time.Second, backoffFactor: 3}

    if next := nextReconnectBackoff(reconnect, 2*time.Second); 6*time.Second != next {
        t.Fatalf("expected 6s, got %s", next)
    }
}

func TestReconnectBackoffShouldReset_ComparesAgainstTheInitialBackoff(t *testing.T) {
    reconnect := resolvedReconnect{initialBackoff: time.Second, maxBackoff: 30 * time.Second, backoffFactor: 2}

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
