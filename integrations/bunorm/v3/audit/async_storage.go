package audit

import (
    "context"
    "sync"
    "sync/atomic"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

const defaultAsyncBufferSize = 1024

type AsyncStorage struct {
    delegate Storage
    queue    chan asyncEntry
    logger   loggingcontract.Logger
    wait     sync.WaitGroup
    mutex    sync.RWMutex
    closed   bool

    loggerMutex sync.RWMutex

    dropped atomic.Uint64
    failed  atomic.Uint64
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

func (instance *AsyncStorage) WithLogger(logger loggingcontract.Logger) *AsyncStorage {
    instance.loggerMutex.Lock()
    instance.logger = logger
    instance.loggerMutex.Unlock()

    return instance
}

func (instance *AsyncStorage) Save(ctx context.Context, table string, entries ...Entry) error {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if true == instance.closed {
        for _, entry := range entries {
            instance.dropped.Add(1)
            instance.deadLetter(entry, exception.NewError("async audit storage is closed, dropped the entry", map[string]any{"table": table}, nil))
        }

        return nil
    }

    for _, entry := range entries {
        select {
        case instance.queue <- asyncEntry{table: table, entry: entry}:
        default:
            instance.dropped.Add(1)
            instance.deadLetter(entry, exception.NewError("async audit queue is full, dropped the entry", map[string]any{"table": table}, nil))
        }
    }

    return nil
}

func (instance *AsyncStorage) run() {
    defer instance.wait.Done()

    for item := range instance.queue {
        if saveErr := instance.delegate.Save(context.Background(), item.table, item.entry); nil != saveErr {
            instance.failed.Add(1)
            instance.deadLetter(item.entry, saveErr)
        }
    }
}

func (instance *AsyncStorage) Dropped() uint64 {
    return instance.dropped.Load()
}

func (instance *AsyncStorage) Failed() uint64 {
    return instance.failed.Load()
}

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
    instance.loggerMutex.RLock()
    logger := instance.logger
    instance.loggerMutex.RUnlock()

    if nil == logger {
        return
    }

    logger.Error("async audit entry could not be stored; dead-lettering", loggingcontract.Context{
        "entity":    entry.Entity,
        "entityId":  entry.EntityId,
        "operation": entry.Operation,
        "changes":   entry.Changes,
        "error":     saveErr.Error(),
    })
}

var _ Storage = (*AsyncStorage)(nil)
