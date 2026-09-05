package contract

import (
    "time"
)

/* Ticker is the contract every clock's NewTicker answers with, and it carries obligations on both sides.

   For the implementer: Stop must be idempotent — consumers stop a ticker from teardown paths that may run more than once — and it must NOT close the channel: time.Ticker leaves its channel open forever, and a consumer that selects on a stopped ticker's channel would spin on the zero value from a closed one. After Stop returns, no new tick may be produced; a tick accepted into the channel before Stop may still be read afterwards, exactly as with time.Ticker.

   For the consumer: Stop is mandatory, not optional. An implementation is allowed to own a relay goroutine (the frozen clock's does), and abandoning such a ticker without Stop leaks that goroutine and pins the ticker and its clock for the life of the process — a cost the system clock's ticker does not have, which is exactly why the contract demands Stop for every implementation. */
type Ticker interface {
    Channel() <-chan time.Time

    Stop()
}
