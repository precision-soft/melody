package bunorm

import (
    "context"

    "github.com/uptrace/bun"

    containercontract "github.com/precision-soft/melody/container/contract"
)

type Provider interface {
    Open(resolver containercontract.Resolver) (*bun.DB, error)
}

/* ContextOpener is the optional capability of opening under a caller's context: the retry loop between attempts sleeps on the context as well as the clock, so a shutdown that cancels it reaches a dial in flight instead of sleeping through the whole retry budget — the exact window in which supervisors send their signals. The registry prefers it whenever the provider implements it, handing the context it was constructed with. */
type ContextOpener interface {
    OpenContext(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error)
}

/* MigrationProvider is the optional capability of opening a connection tuned for migrations: the pool a provider opens for request traffic carries driver-level read and write deadlines sized for requests, and a legitimate DDL statement that runs past them — an ALTER TABLE adding constraints on a large table — is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. A provider that implements this opens the same database with those deadlines lifted; the migration commands prefer it and fall back to the ordinary connection when the capability is absent. */
type MigrationProvider interface {
    OpenForMigration(resolver containercontract.Resolver) (*bun.DB, error)
}
