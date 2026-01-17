package event

import (
	"testing"
	"time"

	"github.com/precision-soft/melody/clock"
	"github.com/precision-soft/melody/internal/testhelper"
)

func TestEvent_StopPropagation(t *testing.T) {
	eventInstance := NewEvent("e", nil, clock.NewSystemClock())

	if true == eventInstance.IsPropagationStopped() {
		t.Fatalf("expected propagation not stopped initially")
	}

	eventInstance.StopPropagation()

	if false == eventInstance.IsPropagationStopped() {
		t.Fatalf("expected propagation stopped")
	}
}

func TestEvent_Constructors(t *testing.T) {
	timestamp := time.Unix(123, 0)

	original := NewEventWithTimestamp("e", "p", timestamp)
	copied := NewEventFromEvent(original)

	if "e" != copied.Name() {
		t.Fatalf("unexpected name")
	}
	if "p" != copied.Payload().(string) {
		t.Fatalf("unexpected payload")
	}
	if timestamp != copied.Timestamp() {
		t.Fatalf("unexpected timestamp")
	}
}

func TestEvent_Constructors_PanicOnEmptyName(t *testing.T) {
	testhelper.AssertPanics(t, func() {
		NewEvent("", nil, clock.NewSystemClock())
	})

	testhelper.AssertPanics(t, func() {
		NewEventWithTimestamp("", nil, time.Now())
	})

	testhelper.AssertPanics(t, func() {
		NewEventFromEvent(NewEventWithTimestamp("", nil, time.Now()))
	})
}
