package audit

import (
    "context"
    "encoding/json"
    "reflect"
    "time"

    "github.com/precision-soft/melody/v4/exception"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* isNilInterface answers true for a typed nil behind an interface too, so a typed-nil storage is refused at construction rather than panicking at the first recorded write */
func isNilInterface(value any) bool {
    if nil == value {
        return true
    }

    reflected := reflect.ValueOf(value)
    switch reflected.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
        return reflected.IsNil()
    default:
        return false
    }
}

/* RecorderConfig declares what a recorder is built with beside its storage: Registry, the entity registry, a registry over DefaultTable when nil; OwnsStorage, whether CloseWithContext closes the storage too, for a composition root that builds both by hand, false on the container path where the storage is a service the container closes itself, since a recorder that closed it too would close it twice; Logger, the dead-letter logger a failed save is journaled through before its error is answered, none when nil, a typed nil refused. */
type RecorderConfig struct {
    Registry    *Registry
    OwnsStorage bool
    Logger      loggingcontract.Logger
}

/* NewRecorder builds a recorder over the storage; a nil or typed-nil storage, a typed-nil Logger and an owned storage carrying Close without CloseWithContext are refused. An AsyncStorage's drain goroutine ends only with its CloseWithContext, so a recorder built by hand over one sets OwnsStorage. */
func NewRecorder(storage Storage, config RecorderConfig) *Recorder {
    if true == isNilInterface(storage) {
        exception.Panic(exception.NewError("audit storage is nil", nil, nil))
    }

    if nil != config.Logger && true == isNilInterface(config.Logger) {
        exception.Panic(exception.NewError("audit recorder logger is nil", map[string]any{"field": "RecorderConfig.Logger"}, nil))
    }

    /* an owned storage is closed through CloseWithContext alone, so one carrying a plain Close would be leaked by the recorder's close */
    if true == config.OwnsStorage && true == storageCarriesOnlyPlainClose(storage) {
        exception.Panic(exception.NewError(
            "an owned audit storage carries Close without CloseWithContext; give it CloseWithContext, or close it yourself and leave OwnsStorage off",
            map[string]any{"field": "RecorderConfig.OwnsStorage"},
            nil,
        ))
    }

    registry := config.Registry
    if nil == registry {
        registry = NewRegistry(RegistryConfig{})
    }

    return &Recorder{
        storage:     storage,
        registry:    registry,
        ownsStorage: config.OwnsStorage,
        logger:      config.Logger,
    }
}

func storageCarriesOnlyPlainClose(storage Storage) bool {
    _, carriesPlainClose := storage.(interface{ Close() error })
    _, carriesContextClose := storage.(interface {
        CloseWithContext(closeContext context.Context) error
    })

    return true == carriesPlainClose && false == carriesContextClose
}

type Recorder struct {
    storage     Storage
    registry    *Registry
    ownsStorage bool
    logger      loggingcontract.Logger
}

func (instance *Recorder) Registry() *Registry {
    return instance.registry
}

/* CloseWithContext hands the teardown's deadline to the storage this recorder owns (RecorderConfig.OwnsStorage), when the storage can be closed at all; a recorder that does not own its storage answers nil so the container can close the storage service itself without a double close. The recorder spends no time of its own. */
func (instance *Recorder) CloseWithContext(closeContext context.Context) error {
    if false == instance.ownsStorage {
        return nil
    }

    contextCloseable, isContextCloseable := instance.storage.(interface {
        CloseWithContext(closeContext context.Context) error
    })
    if false == isContextCloseable {
        return nil
    }

    return contextCloseable.CloseWithContext(closeContext)
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

    if saveErr := instance.storage.Save(ctx, table, []Entry{entry}); nil != saveErr {
        /* a refusal the async storage journaled through this recorder's own logger is skipped, so no entry is journaled twice in one journal; any other refusal, sentinel or not, is dead-lettered here once */
        if false == journaledThrough(saveErr, instance.deadLetterLogger()) {
            instance.deadLetter(table, entry, saveErr)
        }

        return saveErr
    }

    return nil
}

func (instance *Recorder) deadLetterLogger() loggingcontract.Logger {
    return instance.logger
}

func (instance *Recorder) deadLetter(table string, entry Entry, saveErr error) {
    logger := instance.deadLetterLogger()

    if nil == logger {
        return
    }

    /* the record carries the whole cause chain and context, and the change-set as the trail would store it, already redacted: on the unbound path it is the fallback store of the entry, and on a transaction's path it may journal a change-set that was not committed, so a reader replays it against the trail, never against the data */
    logger.Error("audit entry could not be stored; dead-lettering", exception.LogContext(saveErr, exception.LogContextConfig{
        Extra: map[string]any{
            "table":     table,
            "entity":    entry.Entity,
            "entityId":  entry.EntityId,
            "operation": entry.Operation,
            "changes":   entry.Changes,
        },
    }))
}
