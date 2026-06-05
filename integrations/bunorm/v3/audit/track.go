package audit

import (
    "context"
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/uptrace/bun"
)

/**
 * Tracker is the automatic-capture entry point. It executes a bun write and records the matching
 * audit entry in one call, computing the before/after field diff itself. For updates it loads the
 * current row by primary key first, so the change-set has true before values. The write and the
 * audit entry run inside a single transaction (RunInTx), so a failure to persist the audit row rolls
 * the data change back — the data and its audit record are committed together or not at all.
 *
 * This is the Go-native equivalent of the PHP reference's unit-of-work listener: bun exposes no
 * structured changeset and a global QueryHook cannot recover old values for an arbitrary UPDATE,
 * so capture is driven through these helpers (writes must go through the model with WherePK).
 * The lower-level Recorder.Record* API remains available for callers that already hold before/after.
 */
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

/** cloneModel returns a new pointer to the same struct type as model, with model's fields copied in (so its primary key is set for WherePK before the row is re-loaded). */
func cloneModel(model any) (any, error) {
    value := reflect.ValueOf(model)
    if reflect.Ptr != value.Kind() || true == value.IsNil() || reflect.Struct != value.Elem().Kind() {
        return nil, exception.NewError("audited model must be a non-nil pointer to a struct", nil, nil)
    }

    clone := reflect.New(value.Elem().Type())
    clone.Elem().Set(value.Elem())

    return clone.Interface(), nil
}
