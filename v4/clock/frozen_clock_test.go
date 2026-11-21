package clock

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestFrozenClockNow_ReturnsFrozenTime(t *testing.T) {
    frozenTime := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
    clockInstance := NewFrozenClock(frozenTime)

    result := clockInstance.Now()
    if false == result.Equal(frozenTime) {
        t.Fatalf("expected frozen time to be returned")
    }
}

func TestFrozenClockTravelTo_ChangesFrozenTime(t *testing.T) {
    initialTime := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
    targetTime := time.Date(2026, 1, 5, 11, 0, 0, 0, time.UTC)

    clockInstance := NewFrozenClock(initialTime)
    clockInstance.TravelTo(targetTime)

    result := clockInstance.Now()
    if false == result.Equal(targetTime) {
        t.Fatalf("expected traveled time to be returned")
    }
}

func TestFrozenClockAdvance_AddsDuration(t *testing.T) {
    initialTime := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
    clockInstance := NewFrozenClock(initialTime)

    clockInstance.Advance(15 * time.Minute)

    expectedTime := initialTime.Add(15 * time.Minute)
    result := clockInstance.Now()
    if false == result.Equal(expectedTime) {
        t.Fatalf("expected advanced time to be returned")
    }
}

func TestFrozenClockNewTicker_SendsFrozenTime(t *testing.T) {
    initialTime := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
    clockInstance := NewFrozenClock(initialTime)

    tickerInstance := clockInstance.NewTicker(1 * time.Millisecond)
    defer tickerInstance.Stop()

    select {
    case tickTime := <-tickerInstance.Channel():
        if false == tickTime.Equal(initialTime) {
            t.Fatalf("expected ticker to send frozen time")
        }
    case <-time.After(250 * time.Millisecond):
        t.Fatalf("expected ticker to tick")
    }
}

func TestFrozenClockNewTicker_ReflectsTravelToOnNextTick(t *testing.T) {
    initialTime := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
    targetTime := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
    clockInstance := NewFrozenClock(initialTime)

    tickerInstance := clockInstance.NewTicker(1 * time.Millisecond)
    defer tickerInstance.Stop()

    select {
    case <-tickerInstance.Channel():
    case <-time.After(250 * time.Millisecond):
        t.Fatalf("expected ticker to tick")
    }

    clockInstance.TravelTo(targetTime)

    /* a tick fired before TravelTo can already sit in the buffered channel carrying the pre-travel time, so the stale ticks are drained: reading exactly one tick would race the 1ms interval */
    deadline := time.After(250 * time.Millisecond)
    for {
        select {
        case tickTime := <-tickerInstance.Channel():
            if true == tickTime.Equal(targetTime) {
                return
            }
        case <-deadline:
            t.Fatalf("expected ticker to send traveled time")
        }
    }
}

func TestFrozenClockAdvance_RefusesNegativeDuration(t *testing.T) {
    clockInstance := NewFrozenClock(time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC))

    testhelper.AssertPanicsWithError(
        t,
        func() {
            clockInstance.Advance(-1 * time.Hour)
        },
        "invalid advance duration",
    )
}

func TestFrozenClockAdvance_AcceptsZero(t *testing.T) {
    initialTime := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
    clockInstance := NewFrozenClock(initialTime)

    clockInstance.Advance(0)

    if false == clockInstance.Now().Equal(initialTime) {
        t.Fatalf("expected a zero advance to leave the frozen time unchanged")
    }
}

func TestFrozenClockNewTicker_PanicsOnInvalidInterval(t *testing.T) {
    clockInstance := NewFrozenClock(time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC))

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = clockInstance.NewTicker(0)
        },
        "invalid ticker interval",
    )

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = clockInstance.NewTicker(-1 * time.Millisecond)
        },
        "invalid ticker interval",
    )
}

func TestFrozenTickerStop_NoTickMintedAfterStop(t *testing.T) {
    for round := 0; round < 200; round++ {
        initialTime := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
        sentinelTime := time.Date(2030, 6, 1, 0, 0, 0, 0, time.UTC)

        clockInstance := NewFrozenClock(initialTime)
        tickerInstance := clockInstance.NewTicker(1 * time.Nanosecond)

        select {
        case <-tickerInstance.Channel():
        case <-time.After(250 * time.Millisecond):
            t.Fatalf("expected the ticker to tick")
        }

        tickerInstance.Stop()
        clockInstance.TravelTo(sentinelTime)

        for drained := false; false == drained; {
            select {
            case tickTime := <-tickerInstance.Channel():
                if true == tickTime.Equal(sentinelTime) {
                    t.Fatalf("round %d: a tick carried the post-Stop instant — it was minted after Stop returned", round)
                }
            default:
                drained = true
            }
        }
    }
}

/* each round first parks the relay by letting the buffer fill, so the exit path is the one Stop must wait out; the done channel the relay closes on its way out is what makes the wait observable rather than timed. */
func TestFrozenTickerStop_WaitsForTheRelayGoroutine(t *testing.T) {
    for round := 0; round < 100; round++ {
        clockInstance := NewFrozenClock(time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC))
        tickerInstance := clockInstance.NewTicker(1 * time.Nanosecond).(*frozenTicker)

        /* let the buffered channel fill and the relay park in its inner send */
        time.Sleep(2 * time.Millisecond)

        tickerInstance.Stop()

        select {
        case <-tickerInstance.doneChannel:
        default:
            t.Fatalf("round %d: Stop returned before the relay goroutine exited", round)
        }
    }
}

func TestFrozenTickerStop_IsIdempotent(t *testing.T) {
    clockInstance := NewFrozenClock(time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC))
    tickerInstance := clockInstance.NewTicker(1 * time.Millisecond)

    tickerInstance.Stop()
    tickerInstance.Stop()
}

func TestFrozenClock_NewTimerFiresOnWallTimeCarryingTheFrozenNow(t *testing.T) {
    frozenTime := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
    clockInstance := NewFrozenClock(frozenTime)

    timer := clockInstance.NewTimer(20 * time.Millisecond)

    clockInstance.Advance(time.Hour)

    select {
    case fired := <-timer.Channel():
        t.Fatalf("expected Advance not to fire the timer, got %v", fired)
    case <-time.After(5 * time.Millisecond):
    }

    select {
    case fired := <-timer.Channel():
        if false == fired.Equal(frozenTime.Add(time.Hour)) {
            t.Fatalf("expected the frozen Now, got %v", fired)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the timer to fire on wall time")
    }
}

func TestFrozenClock_ATimerStoppedSendsNothingAndResetRearmsIt(t *testing.T) {
    clockInstance := NewFrozenClock(time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC))

    timer := clockInstance.NewTimer(time.Hour)
    if false == timer.Stop() {
        t.Fatalf("expected Stop to answer true for a timer that had not fired")
    }

    timer.Reset(10 * time.Millisecond)

    select {
    case <-timer.Channel():
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the reset timer to fire")
    }
}

func TestFrozenClock_AStoppedTimerNeverFiresAndAFiredOneAnswersFalse(t *testing.T) {
    clockInstance := NewFrozenClock(time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC))

    stopped := clockInstance.NewTimer(20 * time.Millisecond)
    stopped.Stop()

    select {
    case <-stopped.Channel():
        t.Fatalf("expected a stopped timer never to fire")
    case <-time.After(60 * time.Millisecond):
    }

    fired := clockInstance.NewTimer(time.Millisecond)
    <-fired.Channel()

    if true == fired.Stop() {
        t.Fatalf("expected Stop of a fired timer to answer false")
    }
}
