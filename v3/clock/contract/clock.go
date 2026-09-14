package contract

import "time"

/* Clock supplies the current time and tickers. NewTicker requires a positive interval. Frozen-clock tickers run on wall time and stamp each tick with Now at delivery; Advance and TravelTo do not trigger ticks. */
type Clock interface {
    Now() time.Time

    NewTicker(interval time.Duration) Ticker
}
