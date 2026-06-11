package audit

import (
    "context"
    "reflect"

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

        return instance.recorder.RecordInsert(withDatabase(ctx, tx), entity, entityId, model)
    })
}

func (instance *Tracker) Update(ctx context.Context, entity string, entityId string, model any) error {
    return instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        before, cloneErr := cloneModel(model)
        if nil != cloneErr {
            return cloneErr
        }

        if selectErr := tx.NewSelect().Model(before).WherePK().Scan(ctx); nil != selectErr {
            return exception.NewError("audited update could not load the current row", map[string]any{"entity": entity}, selectErr)
        }

        if _, updateErr := tx.NewUpdate().Model(model).WherePK().Exec(ctx); nil != updateErr {
            return exception.NewError("audited update failed", map[string]any{"entity": entity}, updateErr)
        }

        return instance.recorder.RecordUpdate(withDatabase(ctx, tx), entity, entityId, before, model)
    })
}

func (instance *Tracker) Delete(ctx context.Context, entity string, entityId string, model any) error {
    return instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        if _, deleteErr := tx.NewDelete().Model(model).WherePK().Exec(ctx); nil != deleteErr {
            return exception.NewError("audited delete failed", map[string]any{"entity": entity}, deleteErr)
        }

        return instance.recorder.RecordDelete(withDatabase(ctx, tx), entity, entityId, model)
    })
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
