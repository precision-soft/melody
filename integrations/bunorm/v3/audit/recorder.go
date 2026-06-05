package audit

import (
    "context"
    "encoding/json"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func NewRecorder(auditDatabase *bun.DB, table string) *Recorder {
    return NewRecorderWithStorage(NewBunStorage(auditDatabase), NewRegistry(table))
}

func NewRecorderWithStorage(storage Storage, registry *Registry) *Recorder {
    if nil == storage {
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

type Recorder struct {
    storage  Storage
    registry *Registry
    logger   loggingcontract.Logger
}

func (instance *Recorder) WithLogger(logger loggingcontract.Logger) *Recorder {
    instance.logger = logger

    return instance
}

func (instance *Recorder) Registry() *Registry {
    return instance.registry
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
        instance.deadLetter(entry, saveErr)

        return saveErr
    }

    return nil
}

func (instance *Recorder) deadLetter(entry Entry, saveErr error) {
    if nil == instance.logger {
        return
    }

    instance.logger.Error("audit entry could not be stored; dead-lettering", loggingcontract.Context{
        "entity":    entry.Entity,
        "entityId":  entry.EntityId,
        "operation": entry.Operation,
        "changes":   entry.Changes,
        "error":     saveErr.Error(),
    })
}
