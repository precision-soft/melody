package repository

import (
    "context"
    "fmt"
    "time"

    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
    "github.com/uptrace/bun"
)

type catalogReadingRow struct {
    bun.BaseModel `bun:"table:melody_example_v3_catalog_reading,alias:reading"`

    TakenAt      time.Time `bun:"taken_at,pk,notnull,type:timestamptz(6)"`
    Headline     string    `bun:"headline,notnull"`
    Payload      string    `bun:"payload,notnull"`
    ProductCount int       `bun:"product_count,notnull"`
    JournalCount int       `bun:"journal_count,notnull"`
}

func newCatalogReadingRow(reading *CatalogReadingRecord) *catalogReadingRow {
    return &catalogReadingRow{
        TakenAt:      reading.TakenAt,
        Headline:     reading.Headline,
        Payload:      reading.Payload,
        ProductCount: reading.ProductCount,
        JournalCount: reading.JournalCount,
    }
}

func (instance *catalogReadingRow) toRecord() *CatalogReadingRecord {
    return &CatalogReadingRecord{
        TakenAt:      instance.TakenAt,
        Headline:     instance.Headline,
        Payload:      instance.Payload,
        ProductCount: instance.ProductCount,
        JournalCount: instance.JournalCount,
    }
}

func newBunCatalogReadingRepository(database *bun.DB) *bunCatalogReadingRepository {
    return &bunCatalogReadingRepository{database: database}
}

type bunCatalogReadingRepository struct {
    database *bun.DB
}

/* Append inserts one reading and maps typed PostgreSQL duplicate-key errors to the repository’s duplicate-instant message. The primary key enforces uniqueness without a preceding existence query. */
func (instance *bunCatalogReadingRepository) Append(ctx context.Context, reading *CatalogReadingRecord) error {
    if validateErr := validateCatalogReading(reading); nil != validateErr {
        return validateErr
    }

    _, insertErr := instance.database.NewInsert().Model(newCatalogReadingRow(reading)).Exec(ctx)

    return asReadingAlreadyRecorded(insertErr)
}

func (instance *bunCatalogReadingRepository) Recent(ctx context.Context, limit int) ([]*CatalogReadingRecord, error) {
    if 0 >= limit {
        return []*CatalogReadingRecord{}, nil
    }

    rowList := make([]*catalogReadingRow, 0, limit)

    selectErr := instance.database.NewSelect().
        Model(&rowList).
        Order("taken_at DESC").
        Limit(limit).
        Scan(ctx)
    if nil != selectErr {
        return nil, selectErr
    }

    recordList := make([]*CatalogReadingRecord, 0, len(rowList))
    for _, row := range rowList {
        recordList = append(recordList, row.toRecord())
    }

    return recordList, nil
}

func (instance *bunCatalogReadingRepository) Count(ctx context.Context) (int, error) {
    return instance.database.NewSelect().Model((*catalogReadingRow)(nil)).Count(ctx)
}

func asReadingAlreadyRecorded(writeErr error) error {
    if nil == writeErr {
        return nil
    }

    if false == melodypgsql.IsDuplicateKey(writeErr) {
        return writeErr
    }

    return fmt.Errorf("reading already recorded")
}
