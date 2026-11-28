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

    expected := bound.ResolvedBackoff{Initial: time.Second, Max: 30 * time.Second, Factor: 2.0}
    if expected != resolved {
        t.Fatalf("expected %+v, got %+v", expected, resolved)
    }
}

func TestResolveReconnect_AGivenFieldWinsAndTheOthersTakeTheDefault(t *testing.T) {
    resolved, resolveErr := resolveReconnect(ReconnectConfig{Backoff: bound.Backoff{Max: bound.Of(10 * time.Second)}})
    if nil != resolveErr {
        t.Fatalf("expected the config to resolve, got %v", resolveErr)
    }

    expected := bound.ResolvedBackoff{Initial: time.Second, Max: 10 * time.Second, Factor: 2.0}
    if expected != resolved {
        t.Fatalf("expected %+v, got %+v", expected, resolved)
    }
}

func TestResolveReconnect_AdmitsTheBoundaries(t *testing.T) {
    resolved, resolveErr := resolveReconnect(ReconnectConfig{
        Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond), Max: bound.Of(time.Millisecond), Factor: 1},
    })
    if nil != resolveErr {
        t.Fatalf("expected 1ms, an initial equal to the cap and the factor 1 to be admitted, got %v", resolveErr)
    }

    expected := bound.ResolvedBackoff{Initial: time.Millisecond, Max: time.Millisecond, Factor: 1}
    if expected != resolved {
        t.Fatalf("expected %+v, got %+v", expected, resolved)
    }
}

func TestResolveReconnect_RefusesAValueOutsideItsRuleNamingTheField(t *testing.T) {
    cases := map[string]struct {
        config ReconnectConfig
        field  string
    }{
        "unlimited initial backoff":       {config: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.UnlimitedDuration()}}, field: "ReconnectConfig.Backoff.Initial"},
        "sub-millisecond initial backoff": {config: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Microsecond)}}, field: "ReconnectConfig.Backoff.Initial"},
        "unlimited max backoff":           {config: ReconnectConfig{Backoff: bound.Backoff{Max: bound.UnlimitedDuration()}}, field: "ReconnectConfig.Backoff.Max"},
        "sub-millisecond max backoff":     {config: ReconnectConfig{Backoff: bound.Backoff{Max: bound.Of(time.Microsecond)}}, field: "ReconnectConfig.Backoff.Max"},
        "initial above the default cap":   {config: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Minute)}}, field: "ReconnectConfig.Backoff.Initial"},
        "initial above a given cap":       {config: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(5 * time.Second), Max: bound.Of(4 * time.Second)}}, field: "ReconnectConfig.Backoff.Initial"},
        "factor 0.5":                      {config: ReconnectConfig{Backoff: bound.Backoff{Factor: 0.5}}, field: "ReconnectConfig.Backoff.Factor"},
        "negative factor":                 {config: ReconnectConfig{Backoff: bound.Backoff{Factor: -1}}, field: "ReconnectConfig.Backoff.Factor"},
        "NaN factor":                      {config: ReconnectConfig{Backoff: bound.Backoff{Factor: math.NaN()}}, field: "ReconnectConfig.Backoff.Factor"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            _, resolveErr := resolveReconnect(testCase.config)
            expectErrorNamingField(t, resolveErr, testCase.field)
        })
    }
}

func TestResolveReconnect_TheCapRefusalNamesTheCappingField(t *testing.T) {
    _, resolveErr := resolveReconnect(ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Minute)}})

    context := exceptionContextOf(resolveErr)
    if "ReconnectConfig.Backoff.Max" != context["cappedBy"] || "30s" != context["maximum"] {
        t.Fatalf("expected the refusal to name the cap and its 30s value, got %v", context)
    }
}

func TestReconnectAttempts_ShortSubscriptionsGrowTheWaitAndALongOneStartsItOver(t *testing.T) {
    testCases := map[string][]struct {
        subscriptionDuration time.Duration
        expectedWait         time.Duration
        expectedWaits        bool
    }{
        "subscriptions shorter than the initial backoff grow the wait by the factor up to the cap": {
            {subscriptionDuration: 0, expectedWait: time.Second, expectedWaits: true},
            {subscriptionDuration: 10 * time.Microsecond, expectedWait: 2 * time.Second, expectedWaits: true},
            {subscriptionDuration: time.Second - time.Millisecond, expectedWait: 4 * time.Second, expectedWaits: true},
            {subscriptionDuration: 0, expectedWait: 8 * time.Second, expectedWaits: true},
            {subscriptionDuration: 0, expectedWait: 16 * time.Second, expectedWaits: true},
            {subscriptionDuration: 0, expectedWait: 30 * time.Second, expectedWaits: true},
            {subscriptionDuration: 0, expectedWait: 30 * time.Second, expectedWaits: true},
        },
        "a subscription that lived the initial backoff resubscribes at once and starts the count over": {
            {subscriptionDuration: 0, expectedWait: time.Second, expectedWaits: true},
            {subscriptionDuration: 0, expectedWait: 2 * time.Second, expectedWaits: true},
            {subscriptionDuration: time.Second, expectedWait: 0, expectedWaits: false},
            {subscriptionDuration: 0, expectedWait: time.Second, expectedWaits: true},
            {subscriptionDuration: 5 * time.Second, expectedWait: 0, expectedWaits: false},
            {subscriptionDuration: 0, expectedWait: time.Second, expectedWaits: true},
        },
    }

    for name, steps := range testCases {
        t.Run(name, func(t *testing.T) {
            attempts := newReconnectAttempts(bound.ResolvedBackoff{Initial: time.Second, Max: 30 * time.Second, Factor: 2})

            for index, step := range steps {
                wait, waits := attempts.afterSubscription(step.subscriptionDuration)
                if step.expectedWait != wait || step.expectedWaits != waits {
                    t.Fatalf("step %d: expected wait %s (waits %t), got %s (waits %t)", index, step.expectedWait, step.expectedWaits, wait, waits)
                }
            }
        })
    }
}
