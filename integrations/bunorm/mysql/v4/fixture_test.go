package mysql

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

/* singleAttemptRetry keeps a refused dial from being retried under the default backoff */
var singleAttemptRetry = RetryConfig{MaxAttempts: bound.Of(1)}

func expectErrorNamingField(t *testing.T, err error, field string) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected a refusal naming %s, got none", field)
    }

    if field != exception.LogContext(err, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the refusal to name %s, got %v: %v", field, exception.LogContext(err, exception.LogContextConfig{}), err)
    }
}

func expectPanicNamingField(t *testing.T, field string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a refusal naming %s, got no error panic", field)
        }

        expectErrorNamingField(t, recoveredErr, field)
    }()

    callback()
}

type neverOpeningDriver struct{}

func (instance neverOpeningDriver) Open(string) (driver.Conn, error) {
    return nil, errors.New("the test driver never opens")
}

/* releaseRecordingConnector grants every acquire and answers the release at once, or with stallRelease only when its context ends, counting the sessions it dials and ends */
type releaseRecordingConnector struct {
    stallRelease bool

    mutex             sync.Mutex
    connects          int
    closes            int
    releases          int
    releaseContextErr error
}

func (instance *releaseRecordingConnector) Connect(context.Context) (driver.Conn, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.connects++

    return &releaseRecordingConnection{connector: instance}, nil
}

func (instance *releaseRecordingConnector) Driver() driver.Driver {
    return neverOpeningDriver{}
}

func (instance *releaseRecordingConnector) counts() (int, int, int) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.connects, instance.closes, instance.releases
}

type releaseRecordingConnection struct {
    connector *releaseRecordingConnector
}

func (instance *releaseRecordingConnection) Prepare(string) (driver.Stmt, error) {
    return nil, errors.New("the release recording connection prepares nothing")
}

func (instance *releaseRecordingConnection) Close() error {
    instance.connector.mutex.Lock()
    defer instance.connector.mutex.Unlock()

    instance.connector.closes++

    return nil
}

func (instance *releaseRecordingConnection) Begin() (driver.Tx, error) {
    return nil, errors.New("the release recording connection opens no transaction")
}

func (instance *releaseRecordingConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
    if true == strings.Contains(query, "version()") {
        return &singleValueRows{value: "8.0.36"}, nil
    }

    return &singleValueRows{value: int64(1)}, nil
}

func (instance *releaseRecordingConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
    instance.connector.mutex.Lock()
    instance.connector.releases++
    instance.connector.releaseContextErr = ctx.Err()
    stall := instance.connector.stallRelease
    instance.connector.mutex.Unlock()

    /* the drivers refuse a statement whose context is already done before any packet, and so does the double */
    if nil != ctx.Err() {
        return nil, ctx.Err()
    }

    if true == stall {
        <-ctx.Done()

        return nil, ctx.Err()
    }

    return driver.RowsAffected(0), nil
}

type singleValueRows struct {
    value    driver.Value
    consumed bool
}

func (instance *singleValueRows) Columns() []string {
    return []string{"value"}
}

func (instance *singleValueRows) Close() error {
    return nil
}

func (instance *singleValueRows) Next(destination []driver.Value) error {
    if true == instance.consumed {
        return io.EOF
    }

    instance.consumed = true
    destination[0] = instance.value

    return nil
}

func newReleaseRecordingDatabase(t *testing.T, connector *releaseRecordingConnector) *bun.DB {
    t.Helper()

    database := bun.NewDB(sql.OpenDB(connector), mysqldialect.New())
    t.Cleanup(func() {
        _ = database.Close()
    })

    return database
}
