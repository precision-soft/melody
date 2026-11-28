package clock_test

import (
    "errors"
    "io"
    "path/filepath"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/cache"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/event"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http/middleware"
    "github.com/precision-soft/melody/v4/lock"
    "github.com/precision-soft/melody/v4/logging"
    "github.com/precision-soft/melody/v4/security"
    "github.com/precision-soft/melody/v4/security/totp"
    "github.com/precision-soft/melody/v4/session"
)

func TestResolveOptionalClock_NilIsTheSystemClock(t *testing.T) {
    resolved, resolveErr := clock.ResolveOptionalClock(nil, "component", "Config.Clock")
    if nil != resolveErr {
        t.Fatalf("expected no refusal, got %v", resolveErr)
    }

    if _, isSystem := resolved.(*clock.SystemClock); false == isSystem {
        t.Fatalf("expected the system clock, got %T", resolved)
    }
}

func TestResolveOptionalClock_AHandedClockIsKept(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(0, 0))

    resolved, resolveErr := clock.ResolveOptionalClock(frozen, "component", "Config.Clock")
    if nil != resolveErr || clockcontract.Clock(frozen) != resolved {
        t.Fatalf("expected the handed clock, got %v / %v", resolved, resolveErr)
    }
}

func TestResolveOptionalClock_ATypedNilIsRefusedNamingTheField(t *testing.T) {
    var typedNil *clock.FrozenClock

    resolved, resolveErr := clock.ResolveOptionalClock(typedNil, "component", "Config.Clock")
    if nil != resolved {
        t.Fatalf("expected no clock, got %v", resolved)
    }

    requireTypedNilRefusal(t, resolveErr, "component clock is a typed nil", "Config.Clock")
}

func TestOptionalDependencies_NilIsTheDefaultAndATypedNilIsRefused(t *testing.T) {
    var typedNil *clock.FrozenClock

    rows := []struct {
        name    string
        message string
        field   string
        build   func(clockInstance clockcontract.Clock) error
    }{
        {"lock.NewInMemoryLocker", "in-memory locker clock is a typed nil", "InMemoryLockerConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { lock.NewInMemoryLocker(lock.InMemoryLockerConfig{Clock: clockInstance}) })
        }},
        {"security.NewMemoryNonceGuard", "memory nonce guard clock is a typed nil", "MemoryNonceGuardConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { security.NewMemoryNonceGuard(security.MemoryNonceGuardConfig{Clock: clockInstance}) })
        }},
        {"cache.NewInMemoryBackend", "in-memory cache backend clock is a typed nil", "InMemoryBackendConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { cache.NewInMemoryBackend(cache.InMemoryBackendConfig{Clock: clockInstance}) })
        }},
        {"security.NewInMemoryTokenStore", "in-memory token store clock is a typed nil", "InMemoryTokenStoreConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { security.NewInMemoryTokenStore(security.InMemoryTokenStoreConfig{Clock: clockInstance}) })
        }},
        {"security.NewCacheFailedAttemptStore", "failed attempt store clock is a typed nil", "CacheFailedAttemptStoreConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { security.NewCacheFailedAttemptStore(security.CacheFailedAttemptStoreConfig{Clock: clockInstance}) })
        }},
        {"session.NewInMemoryStorage", "session in-memory storage clock is a typed nil", "InMemoryStorageConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { session.NewInMemoryStorage(session.InMemoryStorageConfig{Clock: clockInstance}) })
        }},
        {"session.NewManager", "session manager clock is a typed nil", "ManagerConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Clock: clockInstance}) })
        }},
        {"session.NewFileStorage", "session file storage clock is a typed nil", "FileStorageConfig.Clock", func(clockInstance clockcontract.Clock) error {
            storage, storageErr := session.NewFileStorage(filepath.Join(t.TempDir(), "sessions"), session.FileStorageConfig{Clock: clockInstance})
            if nil != storage {
                _ = storage.CloseWithContext(t.Context())
            }

            return storageErr
        }},
        {"event.NewEventDispatcher", "event dispatcher clock is a typed nil", "EventDispatcherConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clockInstance}) })
        }},
        {"middleware.NewFixedWindowLimiter", "rate limiter clock is a typed nil", "LimiterConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { middleware.NewFixedWindowLimiter("login", 1, time.Minute, middleware.LimiterConfig{Clock: clockInstance}) })
        }},
        {"middleware.NewSlidingWindowLimiter", "rate limiter clock is a typed nil", "LimiterConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { middleware.NewSlidingWindowLimiter("login", 1, time.Minute, middleware.LimiterConfig{Clock: clockInstance}) })
        }},
        {"totp.Options.Resolve", "totp clock is a typed nil", "totp.Options.Clock", func(clockInstance clockcontract.Clock) error {
            _, resolveErr := totp.Options{Clock: clockInstance}.Resolve()

            return resolveErr
        }},
        {"logging.NewJsonLogger", "json logger clock is a typed nil", "JsonLoggerConfig.Clock", func(clockInstance clockcontract.Clock) error {
            return capturePanic(func() { logging.NewJsonLogger(io.Discard, logging.JsonLoggerConfig{Clock: clockInstance}) })
        }},
    }

    for _, row := range rows {
        t.Run(row.name, func(t *testing.T) {
            if buildErr := row.build(nil); nil != buildErr {
                t.Fatalf("expected an absent clock to build, got %v", buildErr)
            }

            requireTypedNilRefusal(t, row.build(typedNil), row.message, row.field)
        })
    }
}

func capturePanic(callback func()) (recoveredErr error) {
    defer func() {
        if recovered := recover(); nil != recovered {
            if panicErr, isError := recovered.(error); true == isError {
                recoveredErr = panicErr

                return
            }

            recoveredErr = errors.New("panicked with a value that is not an error")
        }
    }()

    callback()

    return nil
}

func requireTypedNilRefusal(t *testing.T, refusalErr error, message string, field string) {
    t.Helper()

    var refusal *exception.Error
    if false == errors.As(refusalErr, &refusal) {
        t.Fatalf("expected the refusal %q, got %v", message, refusalErr)
    }

    if message != refusal.Message() {
        t.Fatalf("expected the message %q, got %q", message, refusal.Message())
    }

    if field != refusal.Context()["field"] {
        t.Fatalf("expected the field %q, got %v", field, refusal.Context()["field"])
    }
}
