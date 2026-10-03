package mysql

import (
    "math"
    "testing"
    "time"
)

func TestResolvedDuration(t *testing.T) {
    testCases := []struct {
        name       string
        configured time.Duration
        expected   time.Duration
    }{
        {name: "zero is unset", configured: 0, expected: 7 * time.Second},
        {name: "Unlimited lifts the bound", configured: Unlimited, expected: 0},
        {name: "a negative duration read from configuration lifts the bound", configured: -time.Second, expected: 0},
        {name: "a positive value is kept", configured: 3 * time.Second, expected: 3 * time.Second},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            if resolved := resolvedDuration(testCase.configured, 7*time.Second); testCase.expected != resolved {
                t.Fatalf("expected %v, got %v", testCase.expected, resolved)
            }
        })
    }
}

func TestResolvedOpenConnectionCount(t *testing.T) {
    if 50 != resolvedOpenConnectionCount(0, 50) {
        t.Fatalf("expected a zero cap to take the default")
    }

    if 0 != resolvedOpenConnectionCount(Unlimited, 50) || 0 != resolvedOpenConnectionCount(-3, 50) {
        t.Fatalf("expected a negative cap to resolve to the zero database/sql reads as no cap")
    }

    if 3 != resolvedOpenConnectionCount(3, 50) {
        t.Fatalf("expected a positive cap to be kept")
    }
}

func TestResolvedIdleConnectionCount(t *testing.T) {
    if 25 != resolvedIdleConnectionCount(0, 25) {
        t.Fatalf("expected a zero cap to take the default")
    }

    /* zero would retain no idle connection at all on database/sql, the opposite of lifting the cap */
    if math.MaxInt != resolvedIdleConnectionCount(Unlimited, 25) || math.MaxInt != resolvedIdleConnectionCount(-3, 25) {
        t.Fatalf("expected a negative cap to resolve to the largest count")
    }

    if 2 != resolvedIdleConnectionCount(2, 25) {
        t.Fatalf("expected a positive cap to be kept")
    }
}
