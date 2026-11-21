package bound

import (
    "fmt"
    "math"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/exception"
)

var backoffTestRule = BackoffRule{
    Name:           "TestConfig.Backoff",
    InitialDefault: time.Second,
    MaxDefault:     time.Minute,
    FactorDefault:  2,
}

func TestResolveBackoff_ALeftOutFieldTakesTheRulesDefault(t *testing.T) {
    resolved, resolveErr := ResolveBackoff(Backoff{}, backoffTestRule)
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if (ResolvedBackoff{Initial: time.Second, Max: time.Minute, Factor: 2}) != resolved {
        t.Fatalf("expected every default, got %+v", resolved)
    }

    given, givenErr := ResolveBackoff(Backoff{Initial: Of(2 * time.Second), Max: Of(time.Hour), Factor: 1.5}, backoffTestRule)
    if nil != givenErr {
        t.Fatalf("unexpected error: %v", givenErr)
    }

    if (ResolvedBackoff{Initial: 2 * time.Second, Max: time.Hour, Factor: 1.5}) != given {
        t.Fatalf("expected the given values, got %+v", given)
    }
}

func TestResolveBackoff_RefusesWhatItsRuleRefusesNamingTheField(t *testing.T) {
    testCases := []struct {
        name  string
        value Backoff
        field string
    }{
        {"initial below the minimum", Backoff{Initial: Of(time.Duration(0))}, "TestConfig.Backoff.Initial"},
        {"initial unlimited", Backoff{Initial: UnlimitedDuration()}, "TestConfig.Backoff.Initial"},
        {"max below the minimum", Backoff{Max: Of(-time.Second)}, "TestConfig.Backoff.Max"},
        {"max unlimited", Backoff{Max: UnlimitedDuration()}, "TestConfig.Backoff.Max"},
        {"max below initial", Backoff{Initial: Of(2 * time.Minute)}, "TestConfig.Backoff.Initial"},
        {"factor below 1", Backoff{Factor: 0.5}, "TestConfig.Backoff.Factor"},
        {"factor negative", Backoff{Factor: -2}, "TestConfig.Backoff.Factor"},
        {"factor NaN", Backoff{Factor: math.NaN()}, "TestConfig.Backoff.Factor"},
    }

    for _, testCase := range testCases {
        _, resolveErr := ResolveBackoff(testCase.value, backoffTestRule)
        if nil == resolveErr {
            t.Fatalf("%s: expected a refusal", testCase.name)
        }

        rendered := fmt.Sprintf("%v", exception.LogContext(resolveErr, exception.LogContextConfig{}))
        if false == strings.Contains(rendered, testCase.field) {
            t.Fatalf("%s: expected the refusal to name %s, got %s", testCase.name, testCase.field, rendered)
        }
    }
}

func TestResolveBackoff_AMaxBelowTheInitialNamesBoth(t *testing.T) {
    _, resolveErr := ResolveBackoff(Backoff{Initial: Of(time.Minute), Max: Of(time.Second)}, backoffTestRule)
    if nil == resolveErr {
        t.Fatal("expected a max below the initial refused")
    }

    rendered := fmt.Sprintf("%v", exception.LogContext(resolveErr, exception.LogContextConfig{}))
    if false == strings.Contains(rendered, "TestConfig.Backoff.Initial") || false == strings.Contains(rendered, "TestConfig.Backoff.Max") {
        t.Fatalf("expected the refusal to name both fields, got %s", rendered)
    }

    if _, equalErr := ResolveBackoff(Backoff{Initial: Of(time.Second), Max: Of(time.Second)}, backoffTestRule); nil != equalErr {
        t.Fatalf("expected an initial equal to the max admitted, got %v", equalErr)
    }
}

func TestResolvedBackoff_DelayGrowsByTheFactorAndStopsAtTheCap(t *testing.T) {
    backoff := ResolvedBackoff{Initial: time.Second, Max: 10 * time.Second, Factor: 3}

    testCases := []struct {
        attempt  int
        expected time.Duration
    }{
        {-1, time.Second},
        {0, time.Second},
        {1, time.Second},
        {2, 3 * time.Second},
        {3, 9 * time.Second},
        {4, 10 * time.Second},
        {10000, 10 * time.Second},
        {math.MaxInt, 10 * time.Second},
    }

    for _, testCase := range testCases {
        if delay := backoff.Delay(testCase.attempt); testCase.expected != delay {
            t.Fatalf("attempt %d: expected %s, got %s", testCase.attempt, testCase.expected, delay)
        }
    }
}

func TestResolvedBackoff_AFactorOfOneIsAConstantWait(t *testing.T) {
    backoff := ResolvedBackoff{Initial: 250 * time.Millisecond, Max: time.Hour, Factor: 1}

    for _, attempt := range []int{1, 2, 50, 10000} {
        if 250*time.Millisecond != backoff.Delay(attempt) {
            t.Fatalf("attempt %d: expected the constant 250ms, got %s", attempt, backoff.Delay(attempt))
        }
    }
}

func TestResolvedBackoff_AHugeCapAndFactorNeverOverflowIntoANegativeWait(t *testing.T) {
    backoff := ResolvedBackoff{Initial: time.Second, Max: time.Duration(math.MaxInt64), Factor: 1000}

    for _, attempt := range []int{2, 5, 10, 100, 10000} {
        if delay := backoff.Delay(attempt); 0 >= delay {
            t.Fatalf("attempt %d: expected a positive wait, got %s", attempt, delay)
        }
    }
}

func TestResolvedBackoff_AZeroInitialWaitsTheCapRatherThanSpinning(t *testing.T) {
    backoff := ResolvedBackoff{Initial: 0, Max: time.Second, Factor: 2}

    for _, attempt := range []int{1, 2, 3} {
        if time.Second != backoff.Delay(attempt) {
            t.Fatalf("attempt %d: expected the cap, got %s", attempt, backoff.Delay(attempt))
        }
    }
}
