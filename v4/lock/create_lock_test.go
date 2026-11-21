package lock

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

func TestCreateLock_ALockerAnsweringNoLockIsRefusedNamingTheLock(t *testing.T) {
    createdLock, createErr := CreateLock(&noLockLocker{}, "job:none", bound.Of(time.Second))
    if false == internal.IsNilInterface(createdLock) || false == namesNoLock(createErr) {
        t.Fatalf("expected no lock and the no-lock refusal, got %v %v", createdLock, createErr)
    }

    if "job:none" != exception.LogContext(createErr, exception.LogContextConfig{})["name"] {
        t.Fatalf("expected the refusal to name the lock, got %v", exception.LogContext(createErr, exception.LogContextConfig{}))
    }
}
