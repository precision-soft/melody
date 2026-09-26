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

/* CatalogReadingRecord is one reading of the catalogue as the archive holds it. It is not reporting.CatalogReading, whose FromCache describes the serving of one request, while the archive records what is read and when. */
type CatalogReadingRecord struct {
    TakenAt      time.Time
    Headline     string
    Payload      string
    ProductCount int
    JournalCount int
}

/* CatalogReadingRepository is the archive of catalogue readings: the scheduled refresh appends one and the read door lists the most recent. Every method carries a context and an error, so a listing that cannot reach postgres says so rather than answering an empty archive. */
type CatalogReadingRepository interface {
    /* Append records one reading; a second reading at the same instant is refused with "reading already recorded", since the instant is a reading's identity. */
    Append(ctx context.Context, reading *CatalogReadingRecord) error

    /* Recent lists the newest readings first, at most limit of them; a non-positive limit answers the empty list, never the whole archive. */
    Recent(ctx context.Context, limit int) ([]*CatalogReadingRecord, error)

    /* Count answers how many readings the archive holds. */
    Count(ctx context.Context) (int, error)
}

func MustGetCatalogReadingRepository(resolver melodycontainercontract.Resolver) CatalogReadingRepository {
    return melodycontainer.MustFromResolver[CatalogReadingRepository](resolver, ServiceCatalogReadingRepository)
}

/* NewCatalogReadingRepository answers the postgres-backed archive when an archive connection is configured and the in-memory one otherwise. The migration set is applied under the storage's context, so a SIGTERM during the wait on a held migration lock ends the wait. There is no seeding: an empty archive is the honest state of an application that has taken no reading. */
//melody:service ServiceCatalogReadingRepository
func NewCatalogReadingRepository(storage *persistence.ArchiveStorage) (CatalogReadingRepository, error) {
    if false == storage.IsPersistent() {
        return newInMemoryCatalogReadingRepository(), nil
    }

    migrateErr := migration.EnsureArchiveMigrated(storage.Context(), storage.Database())
    if nil != migrateErr {
        return nil, migrateErr
    }

    return newBunCatalogReadingRepository(storage.Database()), nil
}

/* validateCatalogReading reports the first field the reading fails on, shared by both implementations so they refuse with the same words. The instant is required as the row's identity, and a negative count is refused. */
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
