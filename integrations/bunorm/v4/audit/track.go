package audit

import (
    "context"
    "fmt"
    "reflect"
    "strings"

    "github.com/precision-soft/melody/v4/exception"
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

        /* an autoincrement key is known only after the insert, so the entry's entity_id is derived from the populated model */
        return instance.recorder.RecordInsert(withTransactionForDatabase(ctx, tx, instance.database), entity, instance.resolveEntityId(entityId, model), model)
    })
}

/* runInTx is every Tracker operation's transaction boundary. It refuses a context carrying a caller-made WithDatabase binding, since a second transaction on the same pool deadlocks against the caller's own, and it wraps bun's raw BeginTx and Commit failures so they name the entity and the operation too. */
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

    /* the closure's own failure is already named and travels unchanged; only an error the closure did not produce is one of the two margins. A search, not ==: a storage error of a non-comparable type, a struct carrying a slice, makes == panic */
    if nil != unitErr && true == exception.ChainIs(txErr, unitErr) {
        return txErr
    }

    return exception.NewError("audited write transaction failed", map[string]any{"entity": entity, "operation": operation}, txErr)
}

func (instance *Tracker) Update(ctx context.Context, entity string, entityId string, model any) error {
    return instance.runInTx(ctx, entity, OperationUpdate, func(ctx context.Context, tx bun.Tx) error {
        before, cloneErr := beforeImageModel(instance.database, model)
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

/* Delete records the identifier, the actor and the time of a delete, never the passed model's other fields, which the caller usually leaves zero. An entity whose deleted contents must be recoverable opts into CaptureDeleteBeforeImage. */
func (instance *Tracker) Delete(ctx context.Context, entity string, entityId string, model any) error {
    return instance.runInTx(ctx, entity, OperationDelete, func(ctx context.Context, tx bun.Tx) error {
        var before any

        if true == instance.recorder.registry.capturesDeleteBeforeImageFor(entity) {
            captured, cloneErr := beforeImageModel(instance.database, model)
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
    if reflect.Pointer != value.Kind() || true == value.IsNil() {
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

        for reflect.Pointer == fieldValue.Kind() {
            if true == fieldValue.IsNil() {
                return ""
            }
            fieldValue = fieldValue.Elem()
        }

        if false == fieldValue.IsValid() {
            return ""
        }

        part := fmt.Sprintf("%v", fieldValue.Interface())
        /* a single part cannot make the join ambiguous, so a single-column key is never escaped and a key carrying ":" or "\" keeps joining the trail recorded before the escaping */
        if 1 < len(table.PKs) {
            part = escapeEntityIdPart(part)
        }

        parts = append(parts, part)
    }

    return strings.Join(parts, ":")
}

/* escapeEntityIdPart keeps the ":" join of a composite key unambiguous, so ("a:b","c") and ("a","b:c") derive distinct entity ids; a part carrying neither ":" nor "\" renders unchanged, so existing trails keep joining, and a single-column key is never escaped. */
func escapeEntityIdPart(part string) string {
    part = strings.ReplaceAll(part, `\`, `\\`)

    return strings.ReplaceAll(part, ":", `\:`)
}

/* beforeImageModel answers a model of the same type holding the primary key alone, for the stored row to be read into: a field carried over from the new model would survive the read wherever the column decodes into what is already there, a json map keeping the new keys. */
func beforeImageModel(database *bun.DB, model any) (any, error) {
    value := reflect.ValueOf(model)
    if reflect.Pointer != value.Kind() || true == value.IsNil() || reflect.Struct != value.Elem().Kind() {
        return nil, exception.NewError("audited model must be a non-nil pointer to a struct", nil, nil)
    }

    source := value.Elem()
    image := reflect.New(source.Type())

    for _, primaryKeyField := range database.Table(source.Type()).PKs {
        primaryKeyField.Value(image.Elem()).Set(primaryKeyField.Value(source))
    }

    return image.Interface(), nil
}
