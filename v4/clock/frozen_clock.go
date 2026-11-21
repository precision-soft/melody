package clock

import (
    "sync"
    "time"

    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
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
    /* Advance is forward-only; TravelTo is the door for backwards motion. */
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

/* NewTimer answers a timer on real wall time whose value is the frozen Now of the moment it fires; Advance and TravelTo neither fire nor suppress it. Stop and Reset follow time.Timer since Go 1.23: a value not yet received when they are called is dropped, so no stale value is read after either, and a fired timer whose value was not received answers true. */
func (instance *FrozenClock) NewTimer(duration time.Duration) clockcontract.Timer {
    timerInstance := &frozenTimer{
        clockInstance: instance,
        channel:       make(chan time.Time, 1),
    }

    timerInstance.mutex.Lock()
    defer timerInstance.mutex.Unlock()

    timerInstance.arm(duration)

    return timerInstance
}

var _ clockcontract.Clock = (*FrozenClock)(nil)

type frozenTimer struct {
    clockInstance *FrozenClock
    channel       chan time.Time

    /* mutex orders a firing against Stop and Reset: a firing of an older generation sends nothing */
    mutex      sync.Mutex
    timer      *time.Timer
    generation uint64
}

func (instance *frozenTimer) Channel() <-chan time.Time {
    return instance.channel
}

func (instance *frozenTimer) Stop() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.disarm()
}

func (instance *frozenTimer) Reset(duration time.Duration) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    active := instance.disarm()

    instance.arm(duration)

    return active
}

/* arm starts a firing of a new generation; the caller holds the mutex */
func (instance *frozenTimer) arm(duration time.Duration) {
    instance.generation++
    generation := instance.generation

    instance.timer = time.AfterFunc(duration, func() {
        instance.fire(generation)
    })
}

/* disarm stops the pending firing, retires its generation so one already in flight sends nothing, and drops a value not yet received; it answers whether the timer was pending or held an unreceived value. The caller holds the mutex. */
func (instance *frozenTimer) disarm() bool {
    active := instance.timer.Stop()
    instance.generation++

    select {
    case <-instance.channel:
        active = true
    default:
    }

    return active
}

func (instance *frozenTimer) fire(generation uint64) {
    now := instance.clockInstance.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if generation != instance.generation {
        return
    }

    select {
    case instance.channel <- now:
    default:
    }
}

var _ clockcontract.Timer = (*frozenTimer)(nil)

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

    /* the channel is never closed on stop, as with time.Ticker */
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

/* Stop returns after the relay goroutine has exited, so no tick is produced after it; a tick already buffered may still be read, as with time.Ticker. */
func (instance *frozenTicker) Stop() {
    instance.stopOnce.Do(func() {
        instance.ticker.Stop()
        close(instance.stopChannel)
        <-instance.doneChannel
    })
}

var _ clockcontract.Ticker = (*frozenTicker)(nil)
