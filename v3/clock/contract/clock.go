package contract

import "time"

/* Clock abstracts the current time and the creation of tickers. NewTicker panics on a non-positive interval, and the Ticker it returns must be stopped by the consumer. The frozen clock's ticker runs on real wall time: Advance and TravelTo neither fire nor suppress a tick, and a tick carries the frozen Now at delivery. */
type Clock interface {
    Now() time.Time

    NewTicker(interval time.Duration) Ticker
}
