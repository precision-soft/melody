package cache

import (
    "testing"
    "time"
)

func TestFloorPositiveExpiry(t *testing.T) {
    cases := []struct {
        name     string
        ttl      time.Duration
        expected time.Duration
    }{
        {name: "zero stays zero (no expiry)", ttl: 0, expected: 0},
        {name: "negative stays negative (no expiry)", ttl: -5 * time.Second, expected: -5 * time.Second},
        {name: "sub-millisecond floors to one millisecond", ttl: 500 * time.Microsecond, expected: time.Millisecond},
        {name: "one nanosecond floors to one millisecond", ttl: time.Nanosecond, expected: time.Millisecond},
        {name: "exactly one millisecond is unchanged", ttl: time.Millisecond, expected: time.Millisecond},
        {name: "above one millisecond is unchanged", ttl: 1500 * time.Millisecond, expected: 1500 * time.Millisecond},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            /** A positive sub-millisecond ttl must not reach rueidis .Px verbatim: it would derive PX 0 and
                Redis rejects the whole SET; flooring to one millisecond keeps parity with the in-memory
                backend, which accepts any positive ttl. */
            if floored := floorPositiveExpiry(testCase.ttl); testCase.expected != floored {
                t.Fatalf("floorPositiveExpiry(%v) = %v, expected %v", testCase.ttl, floored, testCase.expected)
            }
        })
    }
}
