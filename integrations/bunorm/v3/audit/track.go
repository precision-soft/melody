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

        /* An autoincrement primary key is only known after the insert, so a caller cannot pass it in advance. Derive it from the now-populated model so the INSERT entry carries the real entity_id (and an entity's history stays queryable by id) instead of an empty string. */
        return instance.recorder.RecordInsert(withTransactionForDatabase(ctx, tx, instance.database), entity, instance.resolveEntityId(entityId, model), model)
    })
}

/* runInTx is every Tracker operation's transaction boundary, and it refuses a context that carries a caller-made WithDatabase binding: that binding means the caller already holds its own open transaction, and the Tracker — which owns a transaction of its own by contract — would then open a SECOND one on the same pool. The failure that follows is not an error but a deadlock: the outer transaction holds the row the inner SELECT ... FOR UPDATE asks for, or N concurrent requests hold all N pool connections while each waits for one more. The two doors are alternatives — a caller inside its own unit of work records through Recorder.Record* with WithDatabase(ctx, tx); the Tracker is for the caller that has no transaction and wants the write and its audit entry in one.

   The margins of the transaction are wrapped here because bun's RunInTx returns the BeginTx and Commit failures raw: everything inside the closure names its entity, and the two edges — the commit deadlock, the connection lost at the last moment — were the only failures that reached the caller with no entity and no operation on them. */
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

    /* the closure's own failure is already named and travels unchanged; only an error the closure did not produce is one of the two margins */
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

/* the caller usually passes only the primary key, so the passed-in model's remaining fields are zero values it never read: recording them would assert them as the deleted row's contents. By default nothing is claimed about them — the trail carries the identifier, the actor and the time, which is what the delete actually knows — and the working database is not charged a read to write an audit row. An entity whose deleted contents must be recoverable opts into CaptureDeleteBeforeImage. */
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

        /* a delete that matched no row removed nothing, so recording it would put a deletion that never happened in the trail; a driver that cannot report the count is trusted rather than second-guessed */
        if affected, affectedErr := result.RowsAffected(); nil == affectedErr && 0 == affected {
            return nil
        }

        return instance.recorder.RecordDelete(withTransactionForDatabase(ctx, tx, instance.database), entity, instance.resolveEntityId(entityId, model), before)
    })
}

/* resolveEntityId keeps a caller-supplied id, or derives it from the model's primary key when none was given (the common case for autoincrement keys, unknown before the insert). */
func (instance *Tracker) resolveEntityId(entityId string, model any) string {
    if "" != entityId {
        return entityId
    }

    return entityIdFromModel(instance.database, model)
}

/* entityIdFromModel reads the bun primary-key value(s) off a model via its table schema; a composite key is joined with ":". Returns "" when the model is not a struct pointer, has no primary key, or a primary-key field is reached through a nil pointer. */
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

/* escapeEntityIdPart keeps the ":" join unambiguous: without it the composite keys ("a:b","c") and ("a","b:c") both derived entity_id "a:b:c" and two different rows shared one history. Keys carrying neither ":" nor "\" — the ordinary case — render exactly as before, so existing trails keep joining. */
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
