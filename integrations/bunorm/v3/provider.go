package bunorm

import (
    "context"

    "github.com/uptrace/bun"

    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* Provider opens the connection pool a definition names. Open answers a fresh pool whose ownership transfers to the caller: the registry closes every database it decides not to keep — one handed back beside an error, one that loses a duplicate migration open, one that lands after Close — so an implementation handing out a shared or memoized *bun.DB would have its live pool closed underneath it. */
type Provider interface {
    Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}

/* ContextOpener opens under the caller’s context. The registry prefers this capability and passes its construction context to the provider. */
type ContextOpener interface {
    OpenContext(ctx context.Context, params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}

/* MigrationProvider is the optional capability of opening a connection tuned for migrations: the pool a provider opens for request traffic carries driver-level read and write deadlines sized for requests, and a legitimate DDL statement that runs past them — an ALTER TABLE adding constraints on a large table — is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. A provider that implements this opens the same database with those deadlines lifted; the migration commands prefer it and fall back to the ordinary connection when the capability is absent. */
type MigrationProvider interface {
    OpenForMigration(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}

/* MigrationContextOpener opens migration connections under the caller’s context. The registry prefers it over MigrationProvider and passes its construction context; providers implementing only MigrationProvider remain supported. */
type MigrationContextOpener interface {
    OpenForMigrationContext(ctx context.Context, params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}
