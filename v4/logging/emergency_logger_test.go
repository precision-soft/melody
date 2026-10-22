package logging

import (
    "sync"
    "testing"
)

func TestEmergencyLogger_ReturnsNonNil(t *testing.T) {
    defer CloseEmergencyLogger()

    logger := EmergencyLogger()
    if nil == logger {
        t.Fatalf("expected logger")
    }
}

func TestEmergencyLogger_ReturnsSameSingletonBetweenCalls(t *testing.T) {
    defer CloseEmergencyLogger()

    first := EmergencyLogger()
    second := EmergencyLogger()

    if first != second {
        t.Fatalf("expected singleton instance")
    }
}

func TestEmergencyLogger_CloseResetsSingletonAndRecreatesOnNextCall(t *testing.T) {
    first := EmergencyLogger()

    CloseEmergencyLogger()

    second := EmergencyLogger()
    defer CloseEmergencyLogger()

    if first == second {
        t.Fatalf("expected fresh instance after close")
    }
}

func TestEmergencyLogger_CloseWhenNotInitializedIsNoop(t *testing.T) {
    CloseEmergencyLogger()
    CloseEmergencyLogger()
}

func TestEmergencyLogger_ConcurrentAccessIsSafe(t *testing.T) {
    defer CloseEmergencyLogger()

    var waitGroup sync.WaitGroup
    iterations := 50

    for workerIndex := 0; workerIndex < 4; workerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                _ = EmergencyLogger()
            }
        }()
    }

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()
        for index := 0; index < iterations/5; index++ {
            CloseEmergencyLogger()
        }
    }()

    waitGroup.Wait()
}
