package audit

import (
    "time"

    "github.com/uptrace/bun"
)

const DefaultTable = "melody_audit"

const (
    OperationInsert = "INSERT"
    OperationUpdate = "UPDATE"
    OperationDelete = "DELETE"
)

type Entry struct {
    bun.BaseModel `bun:"table:melody_audit,alias:melody_audit"`

    Id        int64     `bun:"id,pk,autoincrement"`
    Entity    string    `bun:"entity,notnull"`
    EntityId  string    `bun:"entity_id"`
    Operation string    `bun:"operation,notnull"`
    Changes   string    `bun:"changes,type:text"`
    Actor     string    `bun:"actor"`
    CreatedAt time.Time `bun:"created_at,notnull"`
}
