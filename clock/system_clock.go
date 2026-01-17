package clock

import (
	"time"

	clockcontract "github.com/precision-soft/melody/clock/contract"
	"github.com/precision-soft/melody/exception"
)

func NewSystemClock() *SystemClock {
	return &SystemClock{}
}

type SystemClock struct{}

func (instance *SystemClock) Now() time.Time {
	return time.Now()
}

func (instance *SystemClock) NewTicker(interval time.Duration) clockcontract.Ticker {
	if 0 >= interval {
		exception.Panic(
			exception.NewError("invalid ticker interval", map[string]any{"interval": interval}, nil),
		)
	}

	return newSystemTicker(time.NewTicker(interval))
}

var _ clockcontract.Clock = (*SystemClock)(nil)

func newSystemTicker(ticker *time.Ticker) *systemTicker {
	return &systemTicker{
		ticker: ticker,
	}
}

type systemTicker struct {
	ticker *time.Ticker
}

func (instance *systemTicker) Channel() <-chan time.Time {
	return instance.ticker.C
}

func (instance *systemTicker) Stop() {
	instance.ticker.Stop()
}

var _ clockcontract.Ticker = (*systemTicker)(nil)
