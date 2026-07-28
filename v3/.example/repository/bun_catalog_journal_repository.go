package repository

import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/uptrace/bun"
)

/* catalogJournalRow is the journal as the database holds it. */
type catalogJournalRow struct {
    bun.BaseModel `bun:"table:melody_example_v3_catalog_journal,alias:journal"`

    Id         int64     `bun:"id,pk,autoincrement"`
    Actor      string    `bun:"actor,notnull"`
    Action     string    `bun:"action,notnull"`
    Subject    string    `bun:"subject,notnull"`
    SubjectId  string    `bun:"subject_id,notnull"`
    RecordedAt time.Time `bun:"recorded_at,notnull,type:datetime(6)"`
}

func newCatalogJournalRow(entry *CatalogJournalEntry) *catalogJournalRow {
    return &catalogJournalRow{
        Id:         entry.Id,
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

func (instance *bunCatalogJournalRepository) EnsureSchema(ctx context.Context) error {
    _, createErr := instance.database.
        NewCreateTable().
        Model((*catalogJournalRow)(nil)).
        IfNotExists().
        Exec(ctx)

    return createErr
}

func (instance *bunCatalogJournalRepository) Append(ctx context.Context, entry *CatalogJournalEntry) (*CatalogJournalEntry, error) {
    if nil == entry {
        return nil, fmt.Errorf("journal entry is required")
    }

    if "" == strings.TrimSpace(entry.Action) {
        return nil, fmt.Errorf("action is required")
    }

    if "" == strings.TrimSpace(entry.Subject) {
        return nil, fmt.Errorf("subject is required")
    }

    row := newCatalogJournalRow(entry)

    if "" == strings.TrimSpace(row.Actor) {
        row.Actor = CatalogJournalActorSystem
    }

    if true == row.RecordedAt.IsZero() {
        row.RecordedAt = time.Now()
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


var _ CatalogJournalRepository = (*bunCatalogJournalRepository)(nil)
