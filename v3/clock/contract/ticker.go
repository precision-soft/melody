package contract

import (
    "time"
)

/* Ticker consumers must call Stop, including for implementations with relay goroutines. Stop is idempotent, leaves the channel open, and waits until no new tick can be produced. A tick buffered before Stop may still be received. */
type Ticker interface {
    Channel() <-chan time.Time

    Stop()
}
