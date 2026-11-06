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
        LockName:            "melody:outbox:relay",
        LockTtl:             30 * time.Second,
        VisibilityTimeout:   5 * time.Minute,
        BatchSize:           100,
        MaxAttempts:         12,
        MaxDeliveryAttempts: 24,
        InitialBackoff:      15 * time.Second,
        MaxBackoff:          10 * time.Minute,
        BackoffFactor:       2,
        LockReleaseTimeout:  5 * time.Second,
        ResolutionTimeout:   5 * time.Second,
    }

    if expected != resolved {
        t.Fatalf("expected %+v, got %+v", expected, resolved)
    }
}

func TestResolveRelayConfig_AGivenConfigReachesTheRelay(t *testing.T) {
    relay := NewRelay(RelayConfig{
        Repository:          &fakeRepository{},
        Transport:           &fakeTransport{},
        Codec:               &stringCodec{},
        LockName:            "melody:outbox:custom",
        LockTtl:             bound.Of(time.Second),
        VisibilityTimeout:   bound.Of(2 * time.Second),
        BatchSize:           bound.Of(3),
        MaxAttempts:         bound.Of(4),
        MaxDeliveryAttempts: bound.Of(5),
        InitialBackoff:      bound.Of(6 * time.Millisecond),
        MaxBackoff:          bound.Of(7 * time.Millisecond),
        BackoffFactor:       1.5,
        LockReleaseTimeout:  bound.Of(8 * time.Millisecond),
        ResolutionTimeout:   bound.Of(9 * time.Millisecond),
    })

    config := relay.config
    if "melody:outbox:custom" != config.LockName || time.Second != config.LockTtl || 2*time.Second != config.VisibilityTimeout || 3 != config.BatchSize || 4 != config.MaxAttempts || 5 != config.MaxDeliveryAttempts {
        t.Fatalf("expected the given lease, visibility and counts, got %+v", config)
    }

    if 6*time.Millisecond != config.InitialBackoff || 7*time.Millisecond != config.MaxBackoff || 1.5 != config.BackoffFactor || 8*time.Millisecond != config.LockReleaseTimeout || 9*time.Millisecond != config.ResolutionTimeout {
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
        {name: "initial backoff below the minimum", config: RelayConfig{InitialBackoff: bound.Of(time.Microsecond)}, field: "RelayConfig.InitialBackoff"},
        {name: "initial backoff above the default cap", config: RelayConfig{InitialBackoff: bound.Of(time.Hour)}, field: "RelayConfig.InitialBackoff"},
        {name: "max backoff unlimited", config: RelayConfig{MaxBackoff: bound.UnlimitedDuration()}, field: "RelayConfig.MaxBackoff"},
        {name: "factor below one", config: RelayConfig{BackoffFactor: 0.5}, field: "RelayConfig.BackoffFactor"},
        {name: "factor NaN", config: RelayConfig{BackoffFactor: math.NaN()}, field: "RelayConfig.BackoffFactor"},
        {name: "lock release timeout zero", config: RelayConfig{LockReleaseTimeout: bound.Of(time.Duration(0))}, field: "RelayConfig.LockReleaseTimeout"},
        {name: "lock release timeout unlimited", config: RelayConfig{LockReleaseTimeout: bound.UnlimitedDuration()}, field: "RelayConfig.LockReleaseTimeout"},
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
    _, resolveErr := resolveRelayConfig(RelayConfig{InitialBackoff: bound.Of(time.Second), MaxBackoff: bound.Of(500 * time.Millisecond)})
    expectErrorNamingField(t, resolveErr, "RelayConfig.InitialBackoff")

    if "RelayConfig.MaxBackoff" != exception.LogContext(resolveErr, exception.LogContextConfig{})["cappedBy"] {
        t.Fatalf("expected the refusal to name the cap, got %v", exception.LogContext(resolveErr, exception.LogContextConfig{}))
    }

    if _, equalErr := resolveRelayConfig(RelayConfig{InitialBackoff: bound.Of(time.Second), MaxBackoff: bound.Of(time.Second)}); nil != equalErr {
        t.Fatalf("expected an initial backoff equal to the cap admitted, got %v", equalErr)
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

    errorContext := exception.LogContext(resolveErr, exception.LogContextConfig{})
    if _, hasMinimum := errorContext["minimum"]; true == hasMinimum || "RelayConfig.MaxAttempts" != errorContext["flooredBy"] {
        t.Fatalf("expected a refusal naming MaxAttempts without an unrepresentable minimum, got %v", errorContext)
    }
}

func TestNewRelay_ANilDependencyIsRefusedNamingTheField(t *testing.T) {
    expectPanicNamingField(t, "RelayConfig.Repository", func() {
        NewRelay(RelayConfig{Transport: &fakeTransport{}, Codec: &stringCodec{}})
    })

    expectPanicNamingField(t, "RelayConfig.Transport", func() {
        NewRelay(RelayConfig{Repository: &fakeRepository{}, Codec: &stringCodec{}})
    })

    expectPanicNamingField(t, "RelayConfig.Codec", func() {
        NewRelay(RelayConfig{Repository: &fakeRepository{}, Transport: &fakeTransport{}})
    })
}
