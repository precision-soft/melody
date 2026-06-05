package audit

import (
    "context"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

const defaultAsyncBufferSize = 1024

/**
 * AsyncStorage wraps a Storage and persists entries on a background worker, so an audited write does
 * not block the request path on the underlying store. Entries are queued on a bounded channel; when
 * the queue is full an entry is dead-lettered to the logger instead of blocking the caller, and Save
 * always returns nil so a slow/failing backend never rolls back the business transaction. Close
 * drains the queue and waits for the worker to finish — call it during shutdown.
 *
 * The wrapped store runs on a background context (the request context may already be cancelled), so
 * AsyncStorage must wrap a pool-backed store such as BunStorage or FileStorage, not a request-tx one.
 */
type AsyncStorage struct {
    delegate Storage
    queue    chan asyncEntry
    logger   loggingcontract.Logger
    wait     sync.WaitGroup
    mutex    sync.RWMutex
    closed   bool
}

type asyncEntry struct {
    table string
    entry Entry
}

func NewAsyncStorage(delegate Storage, bufferSize int) *AsyncStorage {
    if nil == delegate {
        exception.Panic(exception.NewError("async audit storage delegate is nil", nil, nil))
    }

    if 0 >= bufferSize {
        bufferSize = defaultAsyncBufferSize
    }

    instance := &AsyncStorage{
        delegate: delegate,
        queue:    make(chan asyncEntry, bufferSize),
    }

    instance.wait.Add(1)
    go instance.run()

    return instance
}

/** WithLogger enables dead-letter logging: entries dropped on overflow or failed by the delegate are logged. */
func (instance *AsyncStorage) WithLogger(logger loggingcontract.Logger) *AsyncStorage {
    instance.logger = logger

    return instance
}

func (instance *AsyncStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if true == instance.closed {
        for _, entry := range entries {
            instance.deadLetter(entry, exception.NewError("async audit storage is closed, dropped the entry", map[string]any{"table": table}, nil))
        }

        return nil
    }

    for _, entry := range entries {
        select {
        case instance.queue <- asyncEntry{table: table, entry: entry}:
        default:
            instance.deadLetter(entry, exception.NewError("async audit queue is full, dropped the entry", map[string]any{"table": table}, nil))
        }
    }

    return nil
}

func (instance *AsyncStorage) run() {
    defer instance.wait.Done()

    for item := range instance.queue {
        if saveErr := instance.delegate.Save(context.Background(), item.table, item.entry); nil != saveErr {
            instance.deadLetter(item.entry, saveErr)
        }
    }
}

/** Close stops accepting new entries, drains the queue and waits for the worker. It is safe to call more than once. */
func (instance *AsyncStorage) Close() error {
    instance.mutex.Lock()
    if false == instance.closed {
        instance.closed = true
        close(instance.queue)
    }
    instance.mutex.Unlock()

    instance.wait.Wait()

    return nil
}

func (instance *AsyncStorage) deadLetter(entry Entry, saveErr error) {
    if nil == instance.logger {
        return
    }

    instance.logger.Error("async audit entry could not be stored; dead-lettering", loggingcontract.Context{
        "entity":    entry.Entity,
        "entityId":  entry.EntityId,
        "operation": entry.Operation,
        "changes":   entry.Changes,
        "error":     saveErr.Error(),
    })
}

var _ Storage = (*AsyncStorage)(nil)
