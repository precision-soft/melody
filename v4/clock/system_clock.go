package clock

import (
    "time"

    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
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

func (instance *SystemClock) NewTimer(duration time.Duration) clockcontract.Timer {
    return &systemTimer{timer: time.NewTimer(duration)}
}

var _ clockcontract.Clock = (*SystemClock)(nil)

type systemTimer struct {
    timer *time.Timer
}

func (instance *systemTimer) Channel() <-chan time.Time {
    return instance.timer.C
}

func (instance *systemTimer) Stop() bool {
    return instance.timer.Stop()
}

func (instance *systemTimer) Reset(duration time.Duration) bool {
    return instance.timer.Reset(duration)
}

var _ clockcontract.Timer = (*systemTimer)(nil)

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
