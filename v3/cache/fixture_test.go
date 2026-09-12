package cache

import (
    "time"

    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
)

/* the cache sweep is the one thing in this package that reads a clock, so its doubles are shared by every test file that builds a backend. */
type cacheTestTicker struct {
    channel chan time.Time
}

func (instance *cacheTestTicker) Channel() <-chan time.Time {
    return instance.channel
}

/* the Ticker contract forbids closing the channel on Stop — a consumer selecting on a stopped ticker's channel would spin on the zero value from a closed one — and demands idempotence; the previous close(instance.channel) survived only because the cleanup loop stops reading before Stop runs */
func (instance *cacheTestTicker) Stop() {}

type cacheTestClock struct {
    now time.Time
}

func (instance *cacheTestClock) Now() time.Time {
    return instance.now
}

func (instance *cacheTestClock) NewTicker(interval time.Duration) clockcontract.Ticker {
    return &cacheTestTicker{
        channel: make(chan time.Time),
    }
}
