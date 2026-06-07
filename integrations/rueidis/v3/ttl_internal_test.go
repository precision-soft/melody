package rueidis

import (
    "testing"
    "time"
)

func TestFloorPositiveMilliseconds_FloorsSubMillisecondToOne(t *testing.T) {
    cases := []struct {
        name     string
        ttl      time.Duration
        expected int64
    }{
        {"sub-millisecond floors to 1", 500 * time.Microsecond, 1},
        {"one nanosecond floors to 1", time.Nanosecond, 1},
        {"exact millisecond preserved", time.Millisecond, 1},
        {"two milliseconds preserved", 2 * time.Millisecond, 2},
        {"one second is 1000ms", time.Second, 1000},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            actual := floorPositiveMilliseconds(testCase.ttl)
            if testCase.expected != actual {
                t.Fatalf("floorPositiveMilliseconds(%v) = %d, want %d", testCase.ttl, actual, testCase.expected)
            }
        })
    }
}
