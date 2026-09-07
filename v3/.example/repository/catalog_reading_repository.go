package repository

import (
    "context"
    "fmt"
    "time"

    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
)

const ServiceCatalogReadingRepository = "service.example.catalog.reading.repository"

/* CatalogReadingRecord is one reading of the catalogue as the archive holds it.

   It is deliberately not reporting.CatalogReading, which carries FromCache: that field says how an answer was SERVED and belongs to the request that asked, while the archive records what was READ and when. A row that remembered whether the reading it came from happened to be cached would be recording a fact about a process that has since exited. */
type CatalogReadingRecord struct {
    TakenAt      time.Time
    Headline     string
    Payload      string
    ProductCount int
    JournalCount int
}

/* CatalogReadingRepository is the archive of catalogue readings: the scheduled refresh appends one, and the read door lists the most recent. It carries a context and an error on every method for the reason its siblings do — one implementation talks to a database, and a listing that cannot reach postgres has to say so rather than answer with an empty archive. */
type CatalogReadingRepository interface {
    /* Append records one reading. A reading already recorded at that instant is refused with "reading already recorded": the instant IS the identity of a reading, so a second row at the same instant would be the same reading twice. */
    Append(ctx context.Context, reading *CatalogReadingRecord) error

    /* Recent lists the newest readings first, at most limit of them. A non-positive limit answers the empty list rather than the whole archive: the caller that asks for nothing gets nothing, and an archive that grows for the life of a volume must never be returned whole by accident. */
    Recent(ctx context.Context, limit int) ([]*CatalogReadingRecord, error)

    /* Count answers how many readings the archive holds, which is what a check states when it wants to know that a refresh landed without caring what it said. */
    Count(ctx context.Context) (int, error)
}

func MustGetCatalogReadingRepository(resolver melodycontainercontract.Resolver) CatalogReadingRepository {
    return melodycontainer.MustFromResolver[CatalogReadingRepository](resolver, ServiceCatalogReadingRepository)
}

/* NewCatalogReadingRepository hands back the archive the environment can actually support: the postgres-backed one when an archive connection was configured, and the in-memory one otherwise — the same choice, made the same way, as the four nomenclature repositories beside it.

   The archive's migration set is applied on the way out, so the first caller finds a table rather than a missing relation. There is no seeding: an archive of readings has no nomenclature to start from, and an empty archive is the honest state of an application that has not taken a reading yet. */
//melody:service ServiceCatalogReadingRepository
func NewCatalogReadingRepository(storage *persistence.ArchiveStorage) (CatalogReadingRepository, error) {
    if false == storage.IsPersistent() {
        return newInMemoryCatalogReadingRepository(), nil
    }

    migrateErr := migration.EnsureArchiveMigrated(context.Background(), storage.Database())
    if nil != migrateErr {
        return nil, migrateErr
    }

    return newBunCatalogReadingRepository(storage.Database()), nil
}

/* validateCatalogReading reports the first field the reading fails on, shared by both implementations so a bad append is refused with the same words whichever one the environment picked. The instant is required because it is the row's identity; the counts are refused when negative because a count is a size. */
func validateCatalogReading(reading *CatalogReadingRecord) error {
    if nil == reading {
        return fmt.Errorf("reading is required")
    }

    if true == reading.TakenAt.IsZero() {
        return fmt.Errorf("taken at is required")
    }

    if "" == reading.Headline {
        return fmt.Errorf("headline is required")
    }

    if 0 > reading.ProductCount || 0 > reading.JournalCount {
        return fmt.Errorf("counts may not be negative")
    }

    return nil
}
