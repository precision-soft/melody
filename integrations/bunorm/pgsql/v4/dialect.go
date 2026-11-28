package pgsql

import (
    "github.com/uptrace/bun/dialect/pgdialect"
)

type dialectWithDefaultSchema struct {
    *pgdialect.Dialect
}

func (instance dialectWithDefaultSchema) DefaultSchema() string {
    return "public"
}
