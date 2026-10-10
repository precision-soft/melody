package repository

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
)

const (
    ServiceCatalogJournalRepository = "service.example.catalog.journal.repository"

    /* the actions a journal entry can carry, the three things that happen to a nomenclature record */
    CatalogJournalActionCreated = "created"
    CatalogJournalActionUpdated = "updated"
    CatalogJournalActionDeleted = "deleted"

    /* the actor of a write made with nobody signed in, a scheduled command or a console run */
    CatalogJournalActorSystem = "system"
)

/* CatalogJournalEntry is one change to the nomenclature: who made it, what they did, to which record, and under which request. RequestId is empty for a change made outside a request, since an invented identifier would claim a correlation nothing can follow. */
type CatalogJournalEntry struct {
    Id         int64
    RequestId  string
    Actor      string
    Action     string
    Subject    string
    SubjectId  string
    RecordedAt time.Time
}

type CatalogJournalRepository interface {
    Append(ctx context.Context, entry *CatalogJournalEntry) (*CatalogJournalEntry, error)

    /* AppendBatch writes everything one request accumulated in a single statement; an empty batch touches nothing. */
    AppendBatch(ctx context.Context, entryList []*CatalogJournalEntry) error

    Latest(ctx context.Context, limit int) ([]*CatalogJournalEntry, error)

    Count(ctx context.Context) (int, error)
}

func MustGetCatalogJournalRepository(resolver melodycontainercontract.Resolver) CatalogJournalRepository {
    return melodycontainer.MustFromResolver[CatalogJournalRepository](resolver, ServiceCatalogJournalRepository)
}

/* NewCatalogJournalRepository answers the database-backed journal when a connection is configured and a process-local one otherwise. It is never absent, because every change to the nomenclature records what it did. */
//melody:service ServiceCatalogJournalRepository
func NewCatalogJournalRepository(storage *persistence.CatalogStorage) (CatalogJournalRepository, error) {
    if false == storage.IsPersistent() {
        return newInMemoryCatalogJournalRepository(), nil
    }

    migrateErr := migration.EnsureMigrated(context.Background(), storage.Database())
    if nil != migrateErr {
        return nil, migrateErr
    }

    return newBunCatalogJournalRepository(storage.Database()), nil
}
