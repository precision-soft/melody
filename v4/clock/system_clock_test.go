package clock

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestSystemClock_Now(t *testing.T) {
    clockInstance := NewSystemClock()

    before := time.Now()
    now := clockInstance.Now()
    after := time.Now()

    if true == now.Before(before) {
        t.Fatalf("expected now to be >= before")
    }
    if true == now.After(after.Add(10*time.Millisecond)) {
        t.Fatalf("unexpected time skew")
    }
}

func TestSystemClock_Ticker(t *testing.T) {
    clockInstance := NewSystemClock()

    ticker := clockInstance.NewTicker(5 * time.Millisecond)
    defer ticker.Stop()

    select {
    case <-ticker.Channel():
        return
    case <-time.After(100 * time.Millisecond):
        t.Fatalf("expected ticker to tick")
    }
}

func TestSystemTickerStopDoesNotPanic(t *testing.T) {
    clockInstance := NewSystemClock()

    ticker := clockInstance.NewTicker(1 * time.Millisecond)
    ticker.Stop()
}

/* the panic is pinned by MESSAGE: recovered value-blind the guard is SHADOWED, because time.NewTicker panics on the same input three frames deeper */
func TestSystemClock_NewTicker_PanicsOnInvalidInterval(t *testing.T) {
    clockInstance := NewSystemClock()

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

func TestSystemClock_NewTimerFiresOnceAfterTheDuration(t *testing.T) {
    timer := NewSystemClock().NewTimer(10 * time.Millisecond)

    select {
    case <-timer.Channel():
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the timer to fire")
    }

    select {
    case <-timer.Channel():
        t.Fatalf("expected the timer to fire once")
    case <-time.After(30 * time.Millisecond):
    }
}

func TestSystemClock_ATimerStoppedBeforeItFiresSendsNothingAndResetRearmsIt(t *testing.T) {
    timer := NewSystemClock().NewTimer(time.Hour)

    if false == timer.Stop() {
        t.Fatalf("expected Stop to answer true for a timer that had not fired")
    }

    select {
    case <-timer.Channel():
        t.Fatalf("expected a stopped timer to send nothing")
    case <-time.After(20 * time.Millisecond):
    }

    if true == timer.Reset(10*time.Millisecond) {
        t.Fatalf("expected Reset of a stopped timer to answer false")
    }

    select {
    case <-timer.Channel():
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the reset timer to fire")
    }
}
