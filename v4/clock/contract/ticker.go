package contract

import (
    "time"
)

/* Ticker is what NewTicker answers. Stop is idempotent, never closes the channel and ends tick production, though a tick accepted before Stop may still be read, as with time.Ticker. Stop is mandatory for the consumer: an implementation may own a relay goroutine that leaks without it. */
type Ticker interface {
    Channel() <-chan time.Time

    Stop()
}
