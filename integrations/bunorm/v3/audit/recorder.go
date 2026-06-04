package audit

import (
    "context"
    "encoding/json"
    "time"

    "github.com/uptrace/bun"

    "github.com/precision-soft/melody/v3/exception"
)

func NewRecorder(auditDatabase *bun.DB, table string) *Recorder {
    if nil == auditDatabase {
        exception.Panic(exception.NewError("audit database is nil", nil, nil))
    }

    if "" == table {
        table = DefaultTable
    }

    return &Recorder{
        auditDatabase: auditDatabase,
        table:         table,
    }
}

type Recorder struct {
    auditDatabase *bun.DB
    table         string
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
    changes := ChangeSet(before, after)

    payload, marshalErr := json.Marshal(changes)
    if nil != marshalErr {
        return exception.NewError("could not encode the audit change-set", map[string]any{"entity": entity}, marshalErr)
    }

    entry := Entry{
        Entity:    entity,
        EntityId:  entityId,
        Operation: operation,
        Changes:   string(payload),
        Actor:     ActorFromContext(ctx),
        CreatedAt: time.Now(),
    }

    _, insertErr := instance.auditDatabase.NewInsert().Model(&entry).ModelTableExpr(instance.table).Exec(ctx)
    if nil != insertErr {
        return exception.NewError("could not write the audit entry", map[string]any{"entity": entity}, insertErr)
    }

    return nil
}
