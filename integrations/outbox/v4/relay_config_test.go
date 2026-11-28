package outbox

import (
    "math"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

func TestResolveRelayConfig_AnEmptyConfigIsEveryRuleDefault(t *testing.T) {
    resolved, resolveErr := resolveRelayConfig(RelayConfig{})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    expected := resolvedRelay{
        LockName:              "",
        LockTtl:               30 * time.Second,
        VisibilityTimeout:     5 * time.Minute,
        BatchSize:             100,
        MaxAttempts:           12,
        MaxDeliveryAttempts:   24,
        Backoff:               bound.ResolvedBackoff{Initial: 15 * time.Second, Max: 10 * time.Minute, Factor: 2},
        ResolutionTimeout:     5 * time.Second,
        UnavailableBackoff:    bound.ResolvedBackoff{Initial: 15 * time.Second, Max: time.Minute, Factor: 2},
    }

    if expected != resolved {
        t.Fatalf("expected %+v, got %+v", expected, resolved)
    }
}

func TestResolveRelayConfig_AGivenConfigReachesTheRelay(t *testing.T) {
    relay := NewRelay(&fakeRepository{}, &fakeTransport{}, &stringCodec{}, RelayConfig{
        LockName:            "melody:outbox:custom",
        LockTtl:             bound.Of(time.Second),
        VisibilityTimeout:   bound.Of(2 * time.Second),
        BatchSize:           bound.Of(3),
        MaxAttempts:         bound.Of(4),
        MaxDeliveryAttempts: bound.Of(5),
        Backoff:             bound.Backoff{Initial: bound.Of(6 * time.Millisecond), Max: bound.Of(7 * time.Millisecond), Factor: 1.5},
        ResolutionTimeout:   bound.Of(9 * time.Millisecond),
    })

    config := relay.config
    if "melody:outbox:custom" != config.LockName || time.Second != config.LockTtl || 2*time.Second != config.VisibilityTimeout || 3 != config.BatchSize || 4 != config.MaxAttempts || 5 != config.MaxDeliveryAttempts {
        t.Fatalf("expected the given lease, visibility and counts, got %+v", config)
    }

    if (bound.ResolvedBackoff{Initial: 6 * time.Millisecond, Max: 7 * time.Millisecond, Factor: 1.5}) != config.Backoff || 9*time.Millisecond != config.ResolutionTimeout {
        t.Fatalf("expected the given backoff and budgets, got %+v", config)
    }
}

func TestResolveRelayConfig_RefusesALimitOutsideItsRuleNamingTheField(t *testing.T) {
    cases := []struct {
        name   string
        config RelayConfig
        field  string
    }{
        {name: "lock ttl below a second", config: RelayConfig{LockTtl: bound.Of(999 * time.Millisecond)}, field: "RelayConfig.LockTtl"},
        {name: "lock ttl unlimited", config: RelayConfig{LockTtl: bound.UnlimitedDuration()}, field: "RelayConfig.LockTtl"},
        {name: "visibility below a second", config: RelayConfig{VisibilityTimeout: bound.Of(999 * time.Millisecond)}, field: "RelayConfig.VisibilityTimeout"},
        {name: "visibility unlimited", config: RelayConfig{VisibilityTimeout: bound.UnlimitedDuration()}, field: "RelayConfig.VisibilityTimeout"},
        {name: "batch size zero", config: RelayConfig{BatchSize: bound.Of(0)}, field: "RelayConfig.BatchSize"},
        {name: "batch size above the maximum", config: RelayConfig{BatchSize: bound.Of(maximumBatchSize + 1)}, field: "RelayConfig.BatchSize"},
        {name: "batch size unlimited", config: RelayConfig{BatchSize: bound.UnlimitedCount()}, field: "RelayConfig.BatchSize"},
        {name: "max attempts zero", config: RelayConfig{MaxAttempts: bound.Of(0)}, field: "RelayConfig.MaxAttempts"},
        {name: "max attempts unlimited", config: RelayConfig{MaxAttempts: bound.UnlimitedCount()}, field: "RelayConfig.MaxAttempts"},
        {name: "max delivery attempts zero", config: RelayConfig{MaxDeliveryAttempts: bound.Of(0)}, field: "RelayConfig.MaxDeliveryAttempts"},
        {name: "max delivery attempts unlimited", config: RelayConfig{MaxDeliveryAttempts: bound.UnlimitedCount()}, field: "RelayConfig.MaxDeliveryAttempts"},
        {name: "max delivery attempts at max attempts", config: RelayConfig{MaxDeliveryAttempts: bound.Of(12)}, field: "RelayConfig.MaxDeliveryAttempts"},
        {name: "initial backoff below the minimum", config: RelayConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Microsecond)}}, field: "RelayConfig.Backoff.Initial"},
        {name: "initial backoff above the default cap", config: RelayConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Hour)}}, field: "RelayConfig.Backoff.Initial"},
        {name: "max backoff unlimited", config: RelayConfig{Backoff: bound.Backoff{Max: bound.UnlimitedDuration()}}, field: "RelayConfig.Backoff.Max"},
        {name: "factor below one", config: RelayConfig{Backoff: bound.Backoff{Factor: 0.5}}, field: "RelayConfig.Backoff.Factor"},
        {name: "factor NaN", config: RelayConfig{Backoff: bound.Backoff{Factor: math.NaN()}}, field: "RelayConfig.Backoff.Factor"},
        {name: "resolution timeout zero", config: RelayConfig{ResolutionTimeout: bound.Of(time.Duration(0))}, field: "RelayConfig.ResolutionTimeout"},
        {name: "resolution timeout unlimited", config: RelayConfig{ResolutionTimeout: bound.UnlimitedDuration()}, field: "RelayConfig.ResolutionTimeout"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            _, resolveErr := resolveRelayConfig(testCase.config)
            expectErrorNamingField(t, resolveErr, testCase.field)
        })
    }
}

func TestResolveRelayConfig_AnInitialBackoffAboveTheCapIsRefusedNamingBoth(t *testing.T) {
    _, resolveErr := resolveRelayConfig(RelayConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(500 * time.Millisecond)}})
    expectErrorNamingField(t, resolveErr, "RelayConfig.Backoff.Initial")

    if "RelayConfig.Backoff.Max" != exception.LogContext(resolveErr, exception.LogContextOptions{})["cappedBy"] {
        t.Fatalf("expected the refusal to name the cap, got %v", exception.LogContext(resolveErr, exception.LogContextOptions{}))
    }

    if _, equalErr := resolveRelayConfig(RelayConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(time.Second)}}); nil != equalErr {
        t.Fatalf("expected an initial backoff equal to the cap admitted, got %v", equalErr)
    }
}

func TestResolveRelayConfig_AnUnavailableBackoffAboveItsMaximumIsRefusedNamingBoth(t *testing.T) {
    _, resolveErr := resolveRelayConfig(RelayConfig{UnavailableBackoff: bound.Backoff{Initial: bound.Of(time.Minute), Max: bound.Of(time.Second)}})
    expectErrorNamingField(t, resolveErr, "RelayConfig.UnavailableBackoff.Initial")

    if "RelayConfig.UnavailableBackoff.Max" != exception.LogContext(resolveErr, exception.LogContextOptions{})["cappedBy"] {
        t.Fatalf("expected the refusal to name the cap, got %v", exception.LogContext(resolveErr, exception.LogContextOptions{}))
    }
}

func TestResolveRelayConfig_TheDerivedMaxDeliveryAttemptsSaturates(t *testing.T) {
    resolved, resolveErr := resolveRelayConfig(RelayConfig{MaxAttempts: bound.Of(math.MaxInt)})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if math.MaxInt != resolved.MaxDeliveryAttempts {
        t.Fatalf("expected the derived cap saturated at math.MaxInt, got %d", resolved.MaxDeliveryAttempts)
    }

    resolved, resolveErr = resolveRelayConfig(RelayConfig{MaxAttempts: bound.Of(math.MaxInt / 2)})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 2*(math.MaxInt/2) != resolved.MaxDeliveryAttempts {
        t.Fatalf("expected twice MaxAttempts at the saturation edge, got %d", resolved.MaxDeliveryAttempts)
    }
}

func TestResolveRelayConfig_AMaxDeliveryAttemptsBesideTheLargestMaxAttemptsNamesNoMinimum(t *testing.T) {
    _, resolveErr := resolveRelayConfig(RelayConfig{MaxAttempts: bound.Of(math.MaxInt), MaxDeliveryAttempts: bound.Of(math.MaxInt)})
    expectErrorNamingField(t, resolveErr, "RelayConfig.MaxDeliveryAttempts")

    errorContext := exception.LogContext(resolveErr, exception.LogContextOptions{})
    if _, hasMinimum := errorContext["minimum"]; true == hasMinimum || "RelayConfig.MaxAttempts" != errorContext["flooredBy"] {
        t.Fatalf("expected a refusal naming MaxAttempts without an unrepresentable minimum, got %v", errorContext)
    }
}

func TestNewRelay_RefusesANilRequiredArgumentByName(t *testing.T) {
    cases := []struct {
        name     string
        build    func()
        argument string
    }{
        {"a nil repository", func() { NewRelay(nil, &fakeTransport{}, &stringCodec{}, RelayConfig{}) }, "repository"},
        {"a nil transport", func() { NewRelay(&fakeRepository{}, nil, &stringCodec{}, RelayConfig{}) }, "transport"},
        {"a nil codec", func() { NewRelay(&fakeRepository{}, &fakeTransport{}, nil, RelayConfig{}) }, "codec"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError {
                    t.Fatalf("expected a refusal naming the %s argument, got no error panic", testCase.argument)
                }

                if context := exception.LogContext(recoveredErr, exception.LogContextOptions{}); testCase.argument != context["argument"] {
                    t.Fatalf("expected a refusal naming the %s argument, got %v", testCase.argument, context)
                }
            }()

            testCase.build()
        })
    }
}

func TestResolveRelayConfig_TheDefaultRetryAndOutageWaitsDoubleToTheirCaps(t *testing.T) {
    resolved, resolveErr := resolveRelayConfig(RelayConfig{})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    retry := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute}
    for index, want := range retry {
        if delay := resolved.Backoff.Delay(index + 1); want != delay {
            t.Fatalf("retry attempt %d: expected %s, got %s", index+1, want, delay)
        }
    }

    outage := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, time.Minute}
    for index, want := range outage {
        if delay := resolved.UnavailableBackoff.Delay(index + 1); want != delay {
            t.Fatalf("outage pass %d: expected %s, got %s", index+1, want, delay)
        }
    }
}

func TestResolveRelayConfig_TheUnavailableBackoffHasItsOwnFactor(t *testing.T) {
    resolved, resolveErr := resolveRelayConfig(RelayConfig{Backoff: bound.Backoff{Factor: 3}, UnavailableBackoff: bound.Backoff{Factor: 1}})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 3 != resolved.Backoff.Factor || 1 != resolved.UnavailableBackoff.Factor {
        t.Fatalf("expected the retry factor 3 beside the outage factor 1, got %v and %v", resolved.Backoff.Factor, resolved.UnavailableBackoff.Factor)
    }

    _, refusedErr := resolveRelayConfig(RelayConfig{UnavailableBackoff: bound.Backoff{Factor: 0.5}})
    expectErrorNamingField(t, refusedErr, "RelayConfig.UnavailableBackoff.Factor")
}
