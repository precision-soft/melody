package contract

import "time"

/* Clock abstracts the current time and the creation of tickers and timers. NewTicker panics on a non-positive interval, and the Ticker it returns must be stopped by the consumer. The frozen clock's ticker and timer run on real wall time: Advance and TravelTo neither fire nor suppress them, and what they send carries the frozen Now of the moment they fire, so a value buffered before Advance or TravelTo still carries the earlier time. */
type Clock interface {
    Now() time.Time

    NewTicker(interval time.Duration) Ticker

    /* NewTimer answers a timer that fires once after the duration; a duration of zero or less fires at once, as time.NewTimer. */
    NewTimer(duration time.Duration) Timer
}

/* Timer is what NewTimer answers; Stop and Reset answer as time.Timer's do. */
type Timer interface {
    Channel() <-chan time.Time

    Stop() bool

    Reset(duration time.Duration) bool
}
