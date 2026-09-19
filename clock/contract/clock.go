package contract

import "time"

/* Clock abstracts the current time and the creation of tickers. NewTicker panics on a non-positive interval, and the Ticker it returns must be stopped by the consumer — see the Ticker contract.

   A word on the frozen clock every test wires: its ticker is driven by REAL wall time — the tick cadence and the frozen timeline are two different timelines. Advance and TravelTo never fire a tick and never suppress one; each tick that does fire carries the frozen clock's current Now at delivery time, so a tick racing a TravelTo may carry either the pre-travel or the post-travel instant. A test that needs a tick must wait out the real interval: the ticker underneath is a real time.Ticker, so advancing the frozen clock past an interval does not make a tick arrive any sooner than it would have. */
type Clock interface {
    Now() time.Time

    NewTicker(interval time.Duration) Ticker
}
