package mysql_test

import (
    "context"
    "database/sql"
    "os"
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
