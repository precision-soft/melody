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

/* isNilInterface answers true for a nil interface AND for a typed-nil value behind one: `nil == storage` alone lets `var s *BunStorage; NewRecorderWithStorage(s, nil)` through the construction guard, and the panic then fires at the first recorded write — deep in a request, far from the wiring that caused it — instead of at the door whose message names it. */
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

/* NewRecorderOwningStorage is NewRecorderWithStorage with the ownership cascade: Close closes the storage. It exists for the composition root that builds the recorder and its storage by hand — an AsyncStorage in particular has a drain goroutine only its Close ends, and a recorder registered as the sole service over it was a closable resource the container teardown could never reach. */
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
    if false == instance.ownsStorage {
        return nil
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
        instance.deadLetter(table, entry, saveErr)

        return saveErr
    }

    return nil
}

func (instance *Recorder) deadLetter(table string, entry Entry, saveErr error) {
    instance.loggerMutex.RLock()
    logger := instance.logger
    instance.loggerMutex.RUnlock()

    if nil == logger {
        return
    }

    /* the record carries the failure's whole cause chain and context, not a flattened message: an exception.Error renders its message alone through Error(), so "could not write the audit entries" without the driver error and the table was the entire diagnostic an operator got for a trail that stopped filling.

       It carries the change-set too, on purpose: the dead-letter is the fallback store of an entry the trail could not keep ("logged, not dropped" is the readme's contract), and without the changes the record would say only that something was lost. What reaches the journal is the change-set as the trail would have stored it — an encrypted column and an audit:"redact" field are already the placeholder — so the trail's own redaction policy is the journal's, and a field that must not reach either is tagged, not filtered here. */
    logger.Error("audit entry could not be stored; dead-lettering", exception.LogContext(saveErr, map[string]any{
        "table":     table,
        "entity":    entry.Entity,
        "entityId":  entry.EntityId,
        "operation": entry.Operation,
        "changes":   entry.Changes,
    }))
}
