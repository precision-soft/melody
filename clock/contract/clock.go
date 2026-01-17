package contract

import "time"

type Clock interface {
	Now() time.Time

	NewTicker(interval time.Duration) Ticker
}
