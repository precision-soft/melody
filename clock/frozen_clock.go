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
    /* Advance is forward-only: a negative duration silently moved the frozen clock backwards and broke the monotonic invariants the code under test relies on — an idle-pruning threshold compared against a now that went back deletes nothing, forever. TravelTo remains the deliberate door for backwards motion. */
    if 0 > duration {
        exception.Panic(
            exception.NewError("invalid advance duration", map[string]any{"duration": duration}, nil),
        )
    }

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
    doneChannel := make(chan struct{})

    tickerInstance := &frozenTicker{
        clockInstance: clockInstance,
        ticker:        ticker,
        channel:       channelInstance,
        stopChannel:   stopChannel,
        doneChannel:   doneChannel,
    }

    /* @important do NOT close channelInstance on stop: time.Ticker (and so systemTicker) leaves its channel open forever, and a consumer that selects on a stopped ticker's channel would spin on the zero value from a closed one. Same interface, same semantics. */
    go func() {
        defer close(doneChannel)

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
    doneChannel   chan struct{}
}

func (instance *frozenTicker) Channel() <-chan time.Time {
    return instance.channel
}

/* Stop returns only after the relay goroutine has exited. Without the wait, a tick already pending inside the runtime ticker could still be taken after Stop returned, its timestamp sampled from the clock AFTER the caller moved on — a Stop-then-TravelTo sequence found the traveled time in the channel of a stopped ticker, a tick minted after teardown that time.Ticker can never produce. A tick accepted into the buffered channel BEFORE Stop may still be read afterwards, exactly as with time.Ticker. */
func (instance *frozenTicker) Stop() {
    instance.stopOnce.Do(func() {
        instance.ticker.Stop()
        close(instance.stopChannel)
        <-instance.doneChannel
    })
}

var _ clockcontract.Ticker = (*frozenTicker)(nil)
