package bunorm

import (
    "context"
    "fmt"
    "sync"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v4/exception"
)

func NewManager(definitionName string, database *bun.DB) *Manager {
    return &Manager{
        definitionName: definitionName,
        database:       database,
    }
}

type Manager struct {
    definitionName string
    database       *bun.DB

    closeOnce sync.Once
    close     *databaseClose
}

func (instance *Manager) DefinitionName() string {
    return instance.definitionName
}

func (instance *Manager) Database() *bun.DB {
    return instance.database
}

/* CloseWithContext closes the database once, every later call waiting on that same close; see startDatabaseClose for why the close runs on a goroutine of its own and what the deadline ends. */
func (instance *Manager) CloseWithContext(closeContext context.Context) error {
    instance.closeOnce.Do(
        func() {
            instance.close = startDatabaseClose(instance.database)
        },
    )

    return instance.close.await(closeContext, instance.definitionName)
}

/* databaseClose is one database close running on a goroutine of its own: err is written before done is closed, so a reader past done reads it. */
type databaseClose struct {
    done chan struct{}
    err  error
}

/* startDatabaseClose closes a database on a goroutine of its own, since sql.DB.Close waits for the connections still checked out and takes no context; a panicking close is recovered there and answered as the close's error, since it runs past the teardown's own containment. A nil database closes as nothing. */
func startDatabaseClose(database *bun.DB) *databaseClose {
    started := &databaseClose{done: make(chan struct{})}

    go func() {
        defer close(started.done)

        defer func() {
            if recoveredValue := recover(); nil != recoveredValue {
                started.err = exception.NewError(
                    "bunorm database close panicked",
                    map[string]any{"panicValue": fmt.Sprintf("%v", recoveredValue)},
                    exception.PanicCause(recoveredValue),
                )
            }
        }()

        if nil != database {
            started.err = database.Close()
        }
    }()

    return started
}

/* await waits for the close up to the deadline: past it the deadline's error is answered while the close goes on in the background, and a close that ended in the same instant is answered as itself, since a select between two ready channels picks at random. */
func (instance *databaseClose) await(closeContext context.Context, name string) error {
    select {
    case <-instance.done:
        return instance.err
    case <-closeContext.Done():
    }

    select {
    case <-instance.done:
        return instance.err
    default:
    }

    return exception.NewError(
        "bunorm database did not close before the close deadline; its close goes on in the background",
        map[string]any{"definitionName": name},
        closeContext.Err(),
    )
}
