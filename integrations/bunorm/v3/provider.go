package bunorm

import (
    "github.com/uptrace/bun"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type Provider interface {
    Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}

/* MigrationProvider is the optional capability of opening a connection tuned for migrations: the pool a provider opens for request traffic carries driver-level read and write deadlines sized for requests, and a legitimate DDL statement that runs past them — an ALTER TABLE adding constraints on a large table — is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. A provider that implements this opens the same database with those deadlines lifted; the migration commands prefer it and fall back to the ordinary connection when the capability is absent. */
type MigrationProvider interface {
    OpenForMigration(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}
