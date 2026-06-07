package mysql_test

import (
    "context"
    "database/sql"
    "os"
    "strconv"
    "testing"

    _ "github.com/go-sql-driver/mysql"
    mysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

func newLockRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestMysqlLock_MutualExclusionAndRelease(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := mysql.NewLocker(database)
    runtimeInstance := newLockRuntime()

    name := "melody_lock_test"

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

func TestMysqlLock_RefreshReportsLostLock(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := mysql.NewLocker(database)
    runtimeInstance := newLockRuntime()

    lock := locker.CreateLock("melody_lock_refresh_test", 0)

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

func TestMysqlLock_ReacquiresAfterRefreshDetectsLostLock(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql lock integration test")
    }

    sqldb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqldb.Close()

    database := bun.NewDB(sqldb, mysqldialect.New())

    locker := mysql.NewLocker(database)
    runtimeInstance := newLockRuntime()

    name := "melody_lock_reacquire"

    lock := locker.CreateLock(name, 0)
    acquired, acquireErr := lock.Acquire(runtimeInstance)
    if nil != acquireErr || false == acquired {
        t.Fatalf("expected acquire to succeed: %v %v", acquired, acquireErr)
    }

    var ownerId sql.NullInt64
    if ownerErr := sqldb.QueryRowContext(runtimeInstance.Context(), "SELECT IS_USED_LOCK(?)", name).Scan(&ownerId); nil != ownerErr {
        t.Fatalf("read lock owner: %v", ownerErr)
    }
    if false == ownerId.Valid {
        t.Fatalf("expected the lock to be held by a session")
    }
    if _, killErr := sqldb.ExecContext(runtimeInstance.Context(), "KILL "+strconv.FormatInt(ownerId.Int64, 10)); nil != killErr {
        t.Logf("kill returned (tolerated): %v", killErr)
    }

    if refreshErr := lock.Refresh(runtimeInstance, 0); nil == refreshErr {
        t.Fatalf("expected refresh to detect the lost lock")
    }

    reacquired, reacquireErr := lock.Acquire(runtimeInstance)
    if nil != reacquireErr || false == reacquired {
        t.Fatalf("expected re-acquire to succeed: %v %v", reacquired, reacquireErr)
    }

    var heldBy sql.NullInt64
    if heldErr := sqldb.QueryRowContext(runtimeInstance.Context(), "SELECT IS_USED_LOCK(?)", name).Scan(&heldBy); nil != heldErr {
        t.Fatalf("read lock holder after re-acquire: %v", heldErr)
    }
    if false == heldBy.Valid {
        t.Fatalf("expected the lock to be genuinely held after re-acquire")
    }

    if releaseErr := lock.Release(runtimeInstance); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }
}
