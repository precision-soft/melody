package repository

import (
    "context"
    "time"

    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/uptrace/bun"
)

const (
    ServiceCatalogNoteRepository = "service.example.catalog.note.repository"
)

/* CatalogNote is the row the database demo writes and reads back. It lives here rather than in the entity package on purpose: the domain entities are cached through a gob serializer and carry no storage concerns, so the one type that does carry them stays beside the repository that maps it. */
type CatalogNote struct {
    bun.BaseModel `bun:"table:melody_example_notes,alias:note"`

    Id         int64     `bun:"id,pk,autoincrement"`
    Note       string    `bun:"note,notnull"`
    RecordedAt time.Time `bun:"recorded_at,notnull"`
}

type CatalogNoteRepository interface {
    /* EnsureSchema creates the demo table when it is absent. The example carries no migration runner, so the demo owns the one table it needs and creates it on the path that uses it. */
    EnsureSchema(ctx context.Context) error

    Append(ctx context.Context, note string, recordedAt time.Time) (*CatalogNote, error)

    FindById(ctx context.Context, id int64) (*CatalogNote, bool, error)

    Latest(ctx context.Context, limit int) ([]*CatalogNote, error)

    Count(ctx context.Context) (int, error)
}

func MustGetCatalogNoteRepository(resolver melodycontainercontract.Resolver) CatalogNoteRepository {
    return melodycontainer.MustFromResolver[CatalogNoteRepository](resolver, ServiceCatalogNoteRepository)
}
