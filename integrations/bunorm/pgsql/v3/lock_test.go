package pgsql

import (
    "context"
    "database/sql"
    "errors"
    "os"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
)

func newLockRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func pgLockDatabase(t *testing.T) *bun.DB {
    t.Helper()

    dsn := os.Getenv("POSTGRES_DSN")
    if "" == dsn {
        t.Skip("POSTGRES_DSN not set; skipping pgsql lock integration test")
    }

    sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
    t.Cleanup(func() {
        sqldb.Close()
    })

    return bun.NewDB(sqldb, pgdialect.New())
}

func TestPgsqlLock_MutualExclusionAndRelease(t *testing.T) {
    locker := NewLocker(pgLockDatabase(t))
    runtimeInstance := newLockRuntime()

    name := "melody_pg_lock_test"

    first := locker.CreateLock(name, 0)
    second := locker.CreateLock(name, 0)

    acquired, acquireErr := first.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected first acquire to succeed: %v %v", acquired, acquireErr)
    }

    contended, contendedErr := second.Acquire(runtimeInstance)
    if nil != contendedErr || true == contended {
        t.Fatalf("expected contention while held: %v %v", contended, contendedErr)
    }

    if releaseErr := first.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    afterRelease, afterReleaseErr := second.Acquire(runtimeInstance)
    if nil != afterReleaseErr || false == afterRelease {
        t.Fatalf("expected acquire after release: %v %v", afterRelease, afterReleaseErr)
    }

    if releaseErr := second.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("second release: %v", releaseErr)
    }
}

func TestPgsqlLock_RefreshReportsLostLock(t *testing.T) {
    locker := NewLocker(pgLockDatabase(t))
    runtimeInstance := newLockRuntime()

    lock := locker.CreateLock("melody_pg_lock_refresh_test", 0)

    if refreshErr := lock.Refresh(runtimeInstance, 0); nil == refreshErr {
        t.Fatalf("expected refresh to fail before the lock is acquired")
    }

    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, 0); nil != refreshErr {
        t.Fatalf("expected refresh to succeed while held: %v", refreshErr)
    }

    if releaseErr := lock.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, 0); nil == refreshErr {
        t.Fatalf("expected refresh to fail after release")
    }
}

/* every lock failure names both spellings: the caller's name and the advisory key halves the server was actually asked for, without which the operator has nothing to match against pg_locks */
func TestPgsqlLock_FailuresNameTheAdvisoryKeyBesideTheName(t *testing.T) {
    sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN("postgres://melody:melody@127.0.0.1:1/melody?sslmode=disable")))
    database := bun.NewDB(sqldb, pgdialect.New())
    if closeErr := database.Close(); nil != closeErr {
        t.Fatal(closeErr)
    }

    lock := NewLocker(database).CreateLock("melody:very:long:lock:name", 0)

    _, acquireErr := lock.Acquire(newLockRuntime())
    if nil == acquireErr {
        t.Fatal("expected the acquire on a closed database to fail")
    }

    var typed *exception.Error
    if false == errors.As(acquireErr, &typed) {
        t.Fatalf("expected an exception.Error, got %T", acquireErr)
    }

    keyHigh, keyLow := advisoryLockKey("melody:very:long:lock:name")
    if "melody:very:long:lock:name" != typed.Context()["name"] || keyHigh != typed.Context()["keyHigh"] || keyLow != typed.Context()["keyLow"] {
        t.Fatalf("expected the context to carry the name and both key halves, got %v", typed.Context())
    }
}
