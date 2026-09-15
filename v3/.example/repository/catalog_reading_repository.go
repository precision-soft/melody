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

/* CatalogReadingRecord stores a catalogue observation and its instant. It excludes response-specific cache metadata. */
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

/* NewCatalogReadingRepository selects PostgreSQL or in-memory storage, applying the archive migration set before returning a persistent repository. The archive starts empty and is not seeded. */
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
