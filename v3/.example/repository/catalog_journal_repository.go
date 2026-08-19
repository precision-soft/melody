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

    /* the actions a journal entry can carry. They are the three things that happen to a nomenclature record, named once so the writers and the readers agree. */
    CatalogJournalActionCreated = "created"
    CatalogJournalActionUpdated = "updated"
    CatalogJournalActionDeleted = "deleted"

    /* the actor a write carries when nobody was signed in — a scheduled command or a console run changes the nomenclature just as a person does, and the journal says which. */
    CatalogJournalActorSystem = "system"
)

/* CatalogJournalEntry is one change to the nomenclature: who made it, what they did, to which record, and under which request.

RequestId is what ties an entry back to the call that caused it. It is empty for a change made outside a request — a scheduled refresh or a console run has no request to be attributed to, and an identifier invented for one would claim a correlation that cannot be followed anywhere.

It lives here rather than in the entity package on purpose: the domain entities are cached through a gob serializer and carry no storage concerns, so the one type that does carry them stays beside the repository that maps it. */
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

    /* AppendBatch writes everything one request accumulated in a single statement. It is what the request-scoped trail flushes into, so a request that changed several records costs one round trip rather than one per change. An empty batch is not an error and touches nothing. */
    AppendBatch(ctx context.Context, entryList []*CatalogJournalEntry) error

    Latest(ctx context.Context, limit int) ([]*CatalogJournalEntry, error)

    Count(ctx context.Context) (int, error)
}

func MustGetCatalogJournalRepository(resolver melodycontainercontract.Resolver) CatalogJournalRepository {
    return melodycontainer.MustFromResolver[CatalogJournalRepository](resolver, ServiceCatalogJournalRepository)
}

/* NewCatalogJournalRepository hands back the journal the environment can actually keep: the database-backed one when a connection was configured, and a process-local one otherwise. It is never absent, because everything that changes the nomenclature records what it did, and an application that could only do that with a database would refuse half its own writes without one. */
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
