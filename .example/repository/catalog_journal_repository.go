package repository

import (
    "context"
    "time"

    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
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

/* CatalogJournalEntry is one change to the nomenclature: who made it, what they did, and to which record. It lives beside the repository that maps it rather than in the entity package, whose entities are cached through a gob serializer and carry no storage concerns. */
type CatalogJournalEntry struct {
    Id         int64
    Actor      string
    Action     string
    Subject    string
    SubjectId  string
    RecordedAt time.Time
}

type CatalogJournalRepository interface {
    Append(ctx context.Context, entry *CatalogJournalEntry) (*CatalogJournalEntry, error)

    /* Latest answers the entries newest first. A limit of zero or below answers all of them. */
    Latest(ctx context.Context, limit int) ([]*CatalogJournalEntry, error)

    Count(ctx context.Context) (int, error)
}

func MustGetCatalogJournalRepository(resolver melodycontainercontract.Resolver) CatalogJournalRepository {
    return melodycontainer.MustFromResolver[CatalogJournalRepository](resolver, ServiceCatalogJournalRepository)
}
