package main

import (
    "database/sql"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
)

/* openPostgres opens a bun.DB over the pgdriver for the given DSN, the same shape the outbox and pgsql
integration tests use. */
func openPostgres(dsn string) *bun.DB {
    sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))

    return bun.NewDB(sqldb, pgdialect.New())
}
