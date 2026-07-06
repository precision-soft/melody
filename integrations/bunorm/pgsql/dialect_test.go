package pgsql

import (
    "testing"

    "github.com/uptrace/bun/dialect/pgdialect"
)

/* @info the dialect wrapper must pin the default schema so bun schema introspection targets public */

func TestDialectWithDefaultSchemaReturnsPublic(t *testing.T) {
    dialect := dialectWithDefaultSchema{
        Dialect: pgdialect.New(),
    }

    if "public" != dialect.DefaultSchema() {
        t.Fatalf("expected the default schema public, got %q", dialect.DefaultSchema())
    }
}
