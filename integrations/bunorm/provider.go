package bunorm

import (
    "context"

    "github.com/uptrace/bun"

    containercontract "github.com/precision-soft/melody/container/contract"
)

/* Provider opens the connection pool a definition names. Open answers a fresh pool whose ownership transfers to the caller: the registry closes every database it decides not to keep — one handed back beside an error, one that loses a duplicate migration open, one that lands after Close — so an implementation handing out a shared or memoized *bun.DB would have its live pool closed underneath it. */
type Provider interface {
    Open(resolver containercontract.Resolver) (*bun.DB, error)
}

/* ContextOpener is the optional capability of opening under a caller's context: the retry loop between attempts sleeps on the context as well as the clock, so a shutdown that cancels it reaches a dial in flight instead of sleeping through the whole retry budget — the exact window in which supervisors send their signals. The registry prefers it whenever the provider implements it, handing the context it was constructed with. */
type ContextOpener interface {
    OpenContext(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error)
}

/* SecretParameterProvider is the optional capability of naming the configuration parameters that hold this provider's credentials. The registry asks every definition at construction and marks each through the configuration's MarkSecret, so a process that never dials, a debug:parameters run above all, still redacts the password; a provider without it is not asked. */
type SecretParameterProvider interface {
    SecretParameterNames() []string
}

/* MigrationProvider is the optional capability of opening a connection tuned for migrations: the pool a provider opens for request traffic carries driver-level read and write deadlines sized for requests, and a legitimate DDL statement that runs past them — an ALTER TABLE adding constraints on a large table — is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. A provider that implements this opens the same database with those deadlines lifted; the migration commands prefer it and fall back to the ordinary connection when the capability is absent. */
type MigrationProvider interface {
    OpenForMigration(resolver containercontract.Resolver) (*bun.DB, error)
}

/* MigrationContextOpener is what ContextOpener is to Open: the migration open under the caller's context, so a db:migrate that receives SIGTERM against a down database refuses at the first cancellable step. The registry prefers it whenever the provider implements it. */
type MigrationContextOpener interface {
    OpenForMigrationContext(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error)
}
