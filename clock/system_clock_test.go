package clock

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/internal/testhelper"
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

/* @info the panic is pinned by message: recovered value-blind, the test kept passing with the guard deleted because time.NewTicker panics on the same input three frames deeper — the deletion of the guard the test exists to prove was invisible */
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
