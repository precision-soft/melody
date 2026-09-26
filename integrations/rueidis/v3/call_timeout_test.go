package rueidis

import (
    "testing"
    "time"
)

func TestResolvedCallTimeout_FallsBackOnANonPositiveTimeoutAndKeepsAPositiveOne(t *testing.T) {
    cases := map[string]struct {
        timeout  time.Duration
        expected time.Duration
    }{
        "zero":           {timeout: 0, expected: 3 * time.Second},
        "negative":       {timeout: -1 * time.Nanosecond, expected: 3 * time.Second},
        "one nanosecond": {timeout: time.Nanosecond, expected: time.Nanosecond},
        "positive":       {timeout: 750 * time.Millisecond, expected: 750 * time.Millisecond},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            if resolved := resolvedCallTimeout(testCase.timeout, 3*time.Second); testCase.expected != resolved {
                t.Fatalf("expected %v, got %v", testCase.expected, resolved)
            }
        })
    }
}
