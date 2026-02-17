package contract

import (
	"time"
)

type Ticker interface {
	Channel() <-chan time.Time

	Stop()
}
