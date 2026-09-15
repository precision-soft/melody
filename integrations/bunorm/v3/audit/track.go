package audit

import (
    "context"
    "fmt"
    "reflect"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/uptrace/bun"
)

func NewTracker(database *bun.DB, recorder *Recorder) *Tracker {
    if nil == database {
        exception.Panic(exception.NewError("audit tracker database is nil", nil, nil))
    }

    if nil == recorder {
        exception.Panic(exception.NewError("audit tracker recorder is nil", nil, nil))
    }

    return &Tracker{database: database, recorder: recorder}
}

type Tracker struct {
    database *bun.DB
    recorder *Recorder
}

func (instance *Tracker) Insert(ctx context.Context, entity string, entityId string, model any) error {
    return instance.runInTx(ctx, entity, OperationInsert, func(ctx context.Context, tx bun.Tx) error {
        if _, insertErr := tx.NewInsert().Model(model).Exec(ctx); nil != insertErr {
            return exception.NewError("audited insert failed", map[string]any{"entity": entity}, insertErr)
        }

        return instance.recorder.RecordInsert(withTransactionForDatabase(ctx, tx, instance.database), entity, instance.resolveEntityId(entityId, model), model)
    })
}

func (instance *Tracker) runInTx(ctx context.Context, entity string, operation string, unitOfWork func(ctx context.Context, tx bun.Tx) error) error {
    if bound, isBound := ctx.Value(databaseContextKey{}).(*boundDatabase); true == isBound && nil != bound && nil == bound.origin && nil != bound.handle {
        return exception.NewError(
            "audit tracker refuses a context bound with WithDatabase: the tracker owns its own transaction, and running it inside the caller's would deadlock the pool — record through Recorder.Record* to join an existing transaction",
            map[string]any{"entity": entity, "operation": operation},
            nil,
        )
    }

    var unitErr error

    txErr := instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        unitErr = unitOfWork(ctx, tx)

        return unitErr
    })

    if nil == txErr {
        return nil
    }

    if txErr == unitErr {
        return txErr
    }

    return exception.NewError("audited write transaction failed", map[string]any{"entity": entity, "operation": operation}, txErr)
}

func (instance *Tracker) Update(ctx context.Context, entity string, entityId string, model any) error {
    return instance.runInTx(ctx, entity, OperationUpdate, func(ctx context.Context, tx bun.Tx) error {
        before, cloneErr := cloneModel(model)
        if nil != cloneErr {
            return cloneErr
        }

        if selectErr := tx.NewSelect().Model(before).WherePK().For("UPDATE").Scan(ctx); nil != selectErr {
            return exception.NewError("audited update could not load the current row", map[string]any{"entity": entity}, selectErr)
        }

        if _, updateErr := tx.NewUpdate().Model(model).WherePK().Exec(ctx); nil != updateErr {
            return exception.NewError("audited update failed", map[string]any{"entity": entity}, updateErr)
        }

        return instance.recorder.RecordUpdate(withTransactionForDatabase(ctx, tx, instance.database), entity, instance.resolveEntityId(entityId, model), before, model)
    })
}

/* Delete records identity, actor and time without assuming zero-valued model fields describe the stored row. Enable CaptureDeleteBeforeImage when deleted field contents must be captured. */
func (instance *Tracker) Delete(ctx context.Context, entity string, entityId string, model any) error {
    return instance.runInTx(ctx, entity, OperationDelete, func(ctx context.Context, tx bun.Tx) error {
        var before any

        if true == instance.recorder.registry.capturesDeleteBeforeImageFor(entity) {
            captured, cloneErr := cloneModel(model)
            if nil != cloneErr {
                return cloneErr
            }

            if selectErr := tx.NewSelect().Model(captured).WherePK().For("UPDATE").Scan(ctx); nil != selectErr {
                return exception.NewError("audited delete could not load the row it is about to delete", map[string]any{"entity": entity}, selectErr)
            }

            before = captured
        }

        result, deleteErr := tx.NewDelete().Model(model).WherePK().Exec(ctx)
        if nil != deleteErr {
            return exception.NewError("audited delete failed", map[string]any{"entity": entity}, deleteErr)
        }

        if affected, affectedErr := result.RowsAffected(); nil == affectedErr && 0 == affected {
            return nil
        }

        return instance.recorder.RecordDelete(withTransactionForDatabase(ctx, tx, instance.database), entity, instance.resolveEntityId(entityId, model), before)
    })
}

func (instance *Tracker) resolveEntityId(entityId string, model any) string {
    if "" != entityId {
        return entityId
    }

    return entityIdFromModel(instance.database, model)
}

func entityIdFromModel(database *bun.DB, model any) string {
    value := reflect.ValueOf(model)
    if reflect.Ptr != value.Kind() || true == value.IsNil() {
        return ""
    }

    structValue := value.Elem()
    if reflect.Struct != structValue.Kind() {
        return ""
    }

    table := database.Table(structValue.Type())
    if nil == table || 0 == len(table.PKs) {
        return ""
    }

    parts := make([]string, 0, len(table.PKs))
    for _, primaryKey := range table.PKs {
        fieldValue, fieldErr := structValue.FieldByIndexErr(primaryKey.StructField.Index)
        if nil != fieldErr {
            return ""
        }

        for reflect.Ptr == fieldValue.Kind() {
            if true == fieldValue.IsNil() {
                return ""
            }
            fieldValue = fieldValue.Elem()
        }

        if false == fieldValue.IsValid() {
            return ""
        }

        parts = append(parts, escapeEntityIdPart(fmt.Sprintf("%v", fieldValue.Interface())))
    }

    return strings.Join(parts, ":")
}

func escapeEntityIdPart(part string) string {
    part = strings.ReplaceAll(part, `\`, `\\`)

    return strings.ReplaceAll(part, ":", `\:`)
}

func cloneModel(model any) (any, error) {
    value := reflect.ValueOf(model)
    if reflect.Ptr != value.Kind() || true == value.IsNil() || reflect.Struct != value.Elem().Kind() {
        return nil, exception.NewError("audited model must be a non-nil pointer to a struct", nil, nil)
    }

    source := value.Elem()
    clone := reflect.New(source.Type())
    target := clone.Elem()
    target.Set(source)

    for index := 0; index < source.NumField(); index++ {
        if false == target.Field(index).CanSet() {
            continue
        }

        decoupleClonedField(target.Field(index), source.Field(index))
    }

    return clone.Interface(), nil
}

func decoupleClonedField(target reflect.Value, source reflect.Value) {
    switch source.Kind() {
    case reflect.Pointer:
        if true == source.IsNil() {
            return
        }

        copied := reflect.New(source.Elem().Type())
        copied.Elem().Set(source.Elem())
        target.Set(copied)

    case reflect.Slice:
        if true == source.IsNil() {
            return
        }

        copied := reflect.MakeSlice(source.Type(), source.Len(), source.Len())
        reflect.Copy(copied, source)
        target.Set(copied)

    case reflect.Map:
        if true == source.IsNil() {
            return
        }

        copied := reflect.MakeMap(source.Type())
        for _, key := range source.MapKeys() {
            copied.SetMapIndex(key, source.MapIndex(key))
        }
        target.Set(copied)
    }
}
