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

    clone := reflect.New(value.Elem().Type())
    clone.Elem().Set(value.Elem())

    return clone.Interface(), nil
}
