package contract

import "time"

/* Clock abstracts the current time and the creation of tickers. NewTicker panics on a non-positive interval, and the Ticker it returns must be stopped by the consumer. The frozen clock's ticker runs on real wall time: Advance and TravelTo neither fire nor suppress a tick, and a tick carries the frozen Now of the moment it fires, so a tick buffered before Advance or TravelTo still carries the earlier time. */
type Clock interface {
    Now() time.Time

    NewTicker(interval time.Duration) Ticker
}
