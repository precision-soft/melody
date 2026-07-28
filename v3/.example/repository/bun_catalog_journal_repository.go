package repository

import (
    "context"
    "strings"
    "time"

    "github.com/uptrace/bun"
)

const catalogJournalTable = "melody_example_v3_catalog_journal"

/* catalogJournalRow is the journal as the database holds it. */
type catalogJournalRow struct {
    bun.BaseModel `bun:"table:melody_example_v3_catalog_journal,alias:journal"`

    Id         int64     `bun:"id,pk,autoincrement"`
    RequestId  string    `bun:"request_id,notnull"`
    Actor      string    `bun:"actor,notnull"`
    Action     string    `bun:"action,notnull"`
    Subject    string    `bun:"subject,notnull"`
    SubjectId  string    `bun:"subject_id,notnull"`
    RecordedAt time.Time `bun:"recorded_at,notnull,type:datetime(6)"`
}

func newCatalogJournalRow(entry *CatalogJournalEntry) *catalogJournalRow {
    return &catalogJournalRow{
        Id:         entry.Id,
        RequestId:  entry.RequestId,
        Actor:      entry.Actor,
        Action:     entry.Action,
        Subject:    entry.Subject,
        SubjectId:  entry.SubjectId,
        RecordedAt: entry.RecordedAt,
    }
}

func (instance *catalogJournalRow) toEntry() *CatalogJournalEntry {
    return &CatalogJournalEntry{
        Id:         instance.Id,
        RequestId:  instance.RequestId,
        Actor:      instance.Actor,
        Action:     instance.Action,
        Subject:    instance.Subject,
        SubjectId:  instance.SubjectId,
        RecordedAt: instance.RecordedAt,
    }
}

func newBunCatalogJournalRepository(database *bun.DB) *bunCatalogJournalRepository {
    return &bunCatalogJournalRepository{database: database}
}

type bunCatalogJournalRepository struct {
    database *bun.DB
}

/* EnsureSchema creates the table when it is absent and adds the columns a table created by an earlier version of this application does not have yet.

The second half is not optional here: the development database outlives the code that created its tables, so a CREATE TABLE IF NOT EXISTS alone leaves an old table standing without the new column and every write then fails on a column the model declares. */
func (instance *bunCatalogJournalRepository) EnsureSchema(ctx context.Context) error {
    _, createErr := instance.database.
        NewCreateTable().
        Model((*catalogJournalRow)(nil)).
        IfNotExists().
        Exec(ctx)
    if nil != createErr {
        return createErr
    }

    return ensureColumn(ctx, instance.database, catalogJournalTable, "request_id", "VARCHAR(255) NOT NULL DEFAULT ''")
}

func (instance *bunCatalogJournalRepository) Append(ctx context.Context, entry *CatalogJournalEntry) (*CatalogJournalEntry, error) {
    row, prepareErr := prepareCatalogJournalRow(entry)
    if nil != prepareErr {
        return nil, prepareErr
    }

    _, insertErr := instance.database.
        NewInsert().
        Model(row).
        Exec(ctx)
    if nil != insertErr {
        return nil, insertErr
    }

    return row.toEntry(), nil
}

/* AppendBatch writes the whole batch in one statement, so a request that changed several records pays one round trip. Every entry is validated before anything is written: a batch that is half refused would leave the journal disagreeing with the changes it is supposed to describe. */
func (instance *bunCatalogJournalRepository) AppendBatch(ctx context.Context, entryList []*CatalogJournalEntry) error {
    if 0 == len(entryList) {
        return nil
    }

    rowList := make([]*catalogJournalRow, 0, len(entryList))

    for _, entry := range entryList {
        row, prepareErr := prepareCatalogJournalRow(entry)
        if nil != prepareErr {
            return prepareErr
        }

        rowList = append(rowList, row)
    }

    _, insertErr := instance.database.
        NewInsert().
        Model(&rowList).
        Exec(ctx)

    return insertErr
}

func (instance *bunCatalogJournalRepository) Latest(ctx context.Context, limit int) ([]*CatalogJournalEntry, error) {
    if 0 >= limit {
        limit = 10
    }

    rowList := make([]*catalogJournalRow, 0, limit)

    selectErr := instance.database.
        NewSelect().
        Model(&rowList).
        Order("id DESC").
        Limit(limit).
        Scan(ctx)
    if nil != selectErr {
        return nil, selectErr
    }

    entries := make([]*CatalogJournalEntry, 0, len(rowList))
    for _, row := range rowList {
        entries = append(entries, row.toEntry())
    }

    return entries, nil
}

func (instance *bunCatalogJournalRepository) Count(ctx context.Context) (int, error) {
    return instance.database.
        NewSelect().
        Model((*catalogJournalRow)(nil)).
        Count(ctx)
}

/* prepareCatalogJournalRow validates one entry and fills in what the writer left to the journal. It is shared by the single and the batch write so both refuse the same entries: a batch that accepted what a single write rejects would be a way around the check rather than a faster path through it. */
func prepareCatalogJournalRow(entry *CatalogJournalEntry) (*catalogJournalRow, error) {
    validationErr := validateCatalogJournalEntry(entry)
    if nil != validationErr {
        return nil, validationErr
    }

    row := newCatalogJournalRow(entry)

    if "" == strings.TrimSpace(row.Actor) {
        row.Actor = CatalogJournalActorSystem
    }

    if true == row.RecordedAt.IsZero() {
        row.RecordedAt = time.Now()
    }

    return row, nil
}

var _ CatalogJournalRepository = (*bunCatalogJournalRepository)(nil)
