package repository

import (
    "context"
    "fmt"
    "time"

    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
    "github.com/uptrace/bun"
)

/* catalogReadingRow is the archive as postgres holds it.

   The table name is a struct TAG, so it cannot read the constant the migration owns — a tag is a literal, and Go has no way to build one from a constant. The two spellings are therefore kept honest by a test rather than by the compiler, which is why TestCatalogReadingRowNamesTheTableTheMigrationCreates exists: it is the only thing standing between this query and a schema that renamed its table. */
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

/* Append writes one reading, and maps the primary key's refusal onto the message the caller is promised.

   The mapping goes through pgsql.IsDuplicateKey rather than through the text of the error, and that matters twice over: the door reads the typed SQLSTATE (23505) through errors.As, so it sees a conflict through the wrapping an exception puts around it and does not answer true for an unrelated error whose message merely contains the digits. Without it an ordinary second refresh inside one clock tick would reach the caller as the driver's raw duplicate-key text through a 500, where the siblings in this package answer a sentence.

   There is no read-then-insert guard of the kind the nomenclature repositories carry, and its absence is the point: those mint an identifier and must check whether the mint collided, while a reading's identity is the instant it was taken at, which the caller already holds. The constraint is the check. */
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

/* asReadingAlreadyRecorded turns the archive's own conflict into the sentence the caller is promised and hands every other failure back untouched, so a connection that dropped mid-insert stays the diagnosis it is rather than being reported as a reading that was already there. */
func asReadingAlreadyRecorded(writeErr error) error {
    if nil == writeErr {
        return nil
    }

    if false == melodypgsql.IsDuplicateKey(writeErr) {
        return writeErr
    }

    return fmt.Errorf("reading already recorded")
}
