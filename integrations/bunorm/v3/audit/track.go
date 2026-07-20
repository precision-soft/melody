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
    return instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        if _, insertErr := tx.NewInsert().Model(model).Exec(ctx); nil != insertErr {
            return exception.NewError("audited insert failed", map[string]any{"entity": entity}, insertErr)
        }

        /* An autoincrement primary key is only known after the insert, so a caller cannot pass it in
           advance. Derive it from the now-populated model so the INSERT entry carries the real
           entity_id (and an entity's history stays queryable by id) instead of an empty string. */
        return instance.recorder.RecordInsert(withDatabase(ctx, tx), entity, instance.resolveEntityId(entityId, model), model)
    })
}

func (instance *Tracker) Update(ctx context.Context, entity string, entityId string, model any) error {
    return instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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

        return instance.recorder.RecordUpdate(withDatabase(ctx, tx), entity, instance.resolveEntityId(entityId, model), before, model)
    })
}

func (instance *Tracker) Delete(ctx context.Context, entity string, entityId string, model any) error {
    return instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        if _, deleteErr := tx.NewDelete().Model(model).WherePK().Exec(ctx); nil != deleteErr {
            return exception.NewError("audited delete failed", map[string]any{"entity": entity}, deleteErr)
        }

        return instance.recorder.RecordDelete(withDatabase(ctx, tx), entity, instance.resolveEntityId(entityId, model), model)
    })
}

/* resolveEntityId keeps a caller-supplied id, or derives it from the model's primary key when none
   was given (the common case for autoincrement keys, unknown before the insert). */
func (instance *Tracker) resolveEntityId(entityId string, model any) string {
    if "" != entityId {
        return entityId
    }

    return entityIdFromModel(instance.database, model)
}

/* entityIdFromModel reads the bun primary-key value(s) off a model via its table schema; a composite
   key is joined with ":". Returns "" when the model is not a struct pointer, has no primary key, or a
   primary-key field is reached through a nil pointer. */
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

        parts = append(parts, fmt.Sprintf("%v", fieldValue.Interface()))
    }

    return strings.Join(parts, ":")
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
