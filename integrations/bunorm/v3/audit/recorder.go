package audit

import (
    "context"
    "encoding/json"
    "reflect"
    "sync"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func isNilInterface(value any) bool {
    if nil == value {
        return true
    }

    reflected := reflect.ValueOf(value)
    switch reflected.Kind() {
    case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
        return reflected.IsNil()
    default:
        return false
    }
}

func NewRecorder(auditDatabase *bun.DB, table string) *Recorder {
    return NewRecorderWithStorage(NewBunStorage(auditDatabase), NewRegistry(table))
}

/* NewRecorderWithStorage builds a recorder that does NOT own the storage it is handed: on the container path both are registered services and the container closes each one itself, so a recorder that closed it too would close it twice. A composition root that builds both by hand and wants the cascade uses NewRecorderOwningStorage. */
func NewRecorderWithStorage(storage Storage, registry *Registry) *Recorder {
    if true == isNilInterface(storage) {
        exception.Panic(exception.NewError("audit storage is nil", nil, nil))
    }

    if nil == registry {
        registry = NewRegistry(DefaultTable)
    }

    return &Recorder{
        storage:  storage,
        registry: registry,
    }
}

/* NewRecorderOwningStorage closes its storage when the recorder closes. Use it when the recorder owns an AsyncStorage worker or another closable storage. */
func NewRecorderOwningStorage(storage Storage, registry *Registry) *Recorder {
    recorder := NewRecorderWithStorage(storage, registry)
    recorder.ownsStorage = true

    return recorder
}

type Recorder struct {
    storage     Storage
    registry    *Registry
    ownsStorage bool

    loggerMutex sync.RWMutex
    logger      loggingcontract.Logger
}

/* WithLogger installs the dead-letter logger under a lock for the same reason AsyncStorage guards its own: the recorder is shared across request goroutines and nothing restricts this setter to boot, so an unguarded write raced every deadLetter read. A typed-nil logger is refused at this door rather than dereferenced inside the very call that reports a failure. */
func (instance *Recorder) WithLogger(logger loggingcontract.Logger) *Recorder {
    if true == isNilInterface(logger) {
        exception.Panic(exception.NewError("audit recorder logger is nil", nil, nil))
    }

    instance.loggerMutex.Lock()
    instance.logger = logger
    instance.loggerMutex.Unlock()

    return instance
}

func (instance *Recorder) Registry() *Registry {
    return instance.registry
}

/* Close closes the storage when this recorder owns it (NewRecorderOwningStorage) and the storage can be closed at all; a recorder that does not own its storage answers nil so the container can close the storage service itself without a double close. */
func (instance *Recorder) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext carries the teardown's deadline through to the storage this recorder owns. The recorder holds no budget of its own — it is a pass-through door — and the storage below it is the one that spends time, so a deadline stopping here would be a deadline the component that needs it never sees. The context-taking form of the storage is preferred exactly the way the container prefers this one. */
func (instance *Recorder) CloseWithContext(closeContext context.Context) error {
    if false == instance.ownsStorage {
        return nil
    }

    contextCloseable, isContextCloseable := instance.storage.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if true == isContextCloseable {
        return contextCloseable.CloseWithContext(closeContext)
    }

    closeable, isCloseable := instance.storage.(interface{ Close() error })
    if false == isCloseable {
        return nil
    }

    return closeable.Close()
}

func (instance *Recorder) RecordInsert(ctx context.Context, entity string, entityId string, after any) error {
    return instance.record(ctx, OperationInsert, entity, entityId, nil, after)
}

func (instance *Recorder) RecordUpdate(ctx context.Context, entity string, entityId string, before any, after any) error {
    return instance.record(ctx, OperationUpdate, entity, entityId, before, after)
}

func (instance *Recorder) RecordDelete(ctx context.Context, entity string, entityId string, before any) error {
    return instance.record(ctx, OperationDelete, entity, entityId, before, nil)
}

func (instance *Recorder) record(
    ctx context.Context,
    operation string,
    entity string,
    entityId string,
    before any,
    after any,
) error {
    changes := changeSetWithIgnore(before, after, instance.registry.ignoredFieldsFor(entity))

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        return exception.NewError("could not encode the audit change-set", map[string]any{"entity": entity}, marshalErr)
    }

    entry := Entry{
        TransactionId: transactionIdFromContext(ctx),
        Entity:        entity,
        EntityId:      entityId,
        Operation:     operation,
        Changes:       string(payload),
        Actor:         ActorFromContext(ctx),
        CreatedAt:     time.Now(),
    }

    table := instance.registry.tableFor(entity)

    if saveErr := instance.storage.Save(ctx, table, entry); nil != saveErr {

        if false == journaledThrough(saveErr, instance.deadLetterLogger()) {
            instance.deadLetter(table, entry, saveErr)
        }

        return saveErr
    }

    return nil
}

func (instance *Recorder) deadLetterLogger() loggingcontract.Logger {
    instance.loggerMutex.RLock()
    defer instance.loggerMutex.RUnlock()

    return instance.logger
}

func (instance *Recorder) deadLetter(table string, entry Entry, saveErr error) {
    logger := instance.deadLetterLogger()

    if nil == logger {
        return
    }

    logger.Error("audit entry could not be stored; dead-lettering", exception.LogContext(saveErr, map[string]any{
        "table":     table,
        "entity":    entry.Entity,
        "entityId":  entry.EntityId,
        "operation": entry.Operation,
        "changes":   entry.Changes,
    }))
}
