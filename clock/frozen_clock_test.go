package clock

import (
	"testing"
	"time"
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

	select {
	case tickTime := <-tickerInstance.Channel():
		if false == tickTime.Equal(targetTime) {
			t.Fatalf("expected ticker to send traveled time")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("expected ticker to tick")
	}
}
