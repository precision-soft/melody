package repository

import (
    "context"
    "time"

    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    "github.com/uptrace/bun"
)

type BunCatalogNoteRepository struct {
    database *bun.DB
}

func NewBunCatalogNoteRepository(database *bun.DB) *BunCatalogNoteRepository {
    return &BunCatalogNoteRepository{database: database}
}

func (instance *BunCatalogNoteRepository) EnsureSchema(ctx context.Context) error {
    _, createErr := instance.database.
        NewCreateTable().
        Model((*CatalogNote)(nil)).
        IfNotExists().
        Exec(ctx)

    return createErr
}

func (instance *BunCatalogNoteRepository) Append(ctx context.Context, note string, recordedAt time.Time) (*CatalogNote, error) {
    record := &CatalogNote{
        Note:       note,
        RecordedAt: recordedAt,
    }

    _, insertErr := instance.database.
        NewInsert().
        Model(record).
        Exec(ctx)
    if nil != insertErr {
        return nil, insertErr
    }

    return record, nil
}

func (instance *BunCatalogNoteRepository) FindById(ctx context.Context, id int64) (*CatalogNote, bool, error) {
    record := &CatalogNote{}

    selectErr := instance.database.
        NewSelect().
        Model(record).
        Where("id = ?", id).
        Limit(1).
        Scan(ctx)
    if nil != selectErr {
        /* a row that is not there is an answer, not a failure: the caller asked whether it exists */
        return nil, false, nil
    }

    return record, true, nil
}

func (instance *BunCatalogNoteRepository) Latest(ctx context.Context, limit int) ([]*CatalogNote, error) {
    if 0 >= limit {
        limit = 10
    }

    records := make([]*CatalogNote, 0, limit)

    selectErr := instance.database.
        NewSelect().
        Model(&records).
        Order("id DESC").
        Limit(limit).
        Scan(ctx)
    if nil != selectErr {
        return nil, selectErr
    }

    return records, nil
}

func (instance *BunCatalogNoteRepository) Count(ctx context.Context) (int, error) {
    return instance.database.
        NewSelect().
        Model((*CatalogNote)(nil)).
        Count(ctx)
}

/* CatalogNoteRepositoryProvider resolves the database by the name the configuration published it under, so the repository stays unaware of how the connection was wired. */
func CatalogNoteRepositoryProvider(databaseServiceName string) func(resolver melodycontainercontract.Resolver) (CatalogNoteRepository, error) {
    return func(resolver melodycontainercontract.Resolver) (CatalogNoteRepository, error) {
        database, databaseErr := melodycontainer.FromResolver[*bun.DB](resolver, databaseServiceName)
        if nil != databaseErr {
            return nil, databaseErr
        }

        return NewBunCatalogNoteRepository(database), nil
    }
}

var _ CatalogNoteRepository = (*BunCatalogNoteRepository)(nil)
