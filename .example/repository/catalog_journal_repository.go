package repository

import (
    "context"
    "time"

    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
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

/* CatalogJournalEntry is one change to the nomenclature: who made it, what they did, and to which record.

It lives here rather than in the entity package on purpose: the domain entities are cached through a gob serializer and carry no storage concerns, so the one type that does carry them stays beside the repository that maps it. */
type CatalogJournalEntry struct {
    Id         int64
    Actor      string
    Action     string
    Subject    string
    SubjectId  string
    RecordedAt time.Time
}

type CatalogJournalRepository interface {
    /* EnsureSchema creates the journal table when it is absent. The example carries no migration runner, so the repository owns the one table it writes and creates it on the path that reaches it. */
    EnsureSchema(ctx context.Context) error

    Append(ctx context.Context, entry *CatalogJournalEntry) (*CatalogJournalEntry, error)

    Latest(ctx context.Context, limit int) ([]*CatalogJournalEntry, error)

    Count(ctx context.Context) (int, error)
}

func MustGetCatalogJournalRepository(resolver melodycontainercontract.Resolver) CatalogJournalRepository {
    return melodycontainer.MustFromResolver[CatalogJournalRepository](resolver, ServiceCatalogJournalRepository)
}
