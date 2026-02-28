package clock

import (
    "sync"
    "time"

    clockcontract "github.com/precision-soft/melody/clock/contract"
    "github.com/precision-soft/melody/exception"
)

func NewFrozenClock(frozenTime time.Time) *FrozenClock {
    return &FrozenClock{
        currentTime: frozenTime,
    }
}

type FrozenClock struct {
    mutex       sync.RWMutex
    currentTime time.Time
}

func (instance *FrozenClock) Now() time.Time {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.currentTime
}

func (instance *FrozenClock) TravelTo(targetTime time.Time) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.currentTime = targetTime
}

func (instance *FrozenClock) Advance(duration time.Duration) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.currentTime = instance.currentTime.Add(duration)
}

func (instance *FrozenClock) NewTicker(interval time.Duration) clockcontract.Ticker {
    if 0 >= interval {
        exception.Panic(
            exception.NewError("invalid ticker interval", map[string]any{"interval": interval}, nil),
        )
    }

    return newFrozenTicker(instance, time.NewTicker(interval))
}

var _ clockcontract.Clock = (*FrozenClock)(nil)

func newFrozenTicker(clockInstance *FrozenClock, ticker *time.Ticker) *frozenTicker {
    channelInstance := make(chan time.Time, 1)
    stopChannel := make(chan struct{})

    tickerInstance := &frozenTicker{
        clockInstance: clockInstance,
        ticker:        ticker,
        channel:       channelInstance,
        stopChannel:   stopChannel,
    }

    go func() {
        defer close(channelInstance)

        for {
            select {
            case <-ticker.C:
                now := clockInstance.Now()

                select {
                case channelInstance <- now:
                case <-stopChannel:
                    return
                }

            case <-stopChannel:
                return
            }
        }
    }()

    return tickerInstance
}

type frozenTicker struct {
    clockInstance *FrozenClock
    stopOnce      sync.Once
    ticker        *time.Ticker
    channel       chan time.Time
    stopChannel   chan struct{}
}

func (instance *frozenTicker) Channel() <-chan time.Time {
    return instance.channel
}

func (instance *frozenTicker) Stop() {
    instance.stopOnce.Do(func() {
        instance.ticker.Stop()
        close(instance.stopChannel)
    })
}

var _ clockcontract.Ticker = (*frozenTicker)(nil)
