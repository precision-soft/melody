package application

import (
    "testing"
    "time"
)

func TestNewProcessContext_CarriesTheIdentityItWasBuiltWith(t *testing.T) {
    startedAt := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)

    processContext := NewProcessContext("process-1", startedAt)

    if "process-1" != processContext.ProcessId() {
        t.Fatalf("expected the process id back, got %q", processContext.ProcessId())
    }

    if false == startedAt.Equal(processContext.StartedAt()) {
        t.Fatalf("expected the start moment back, got %v", processContext.StartedAt())
    }
}
