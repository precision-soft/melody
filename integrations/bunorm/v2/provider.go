package bunorm

import (
    "context"

    "github.com/uptrace/bun"

    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

/* Provider opens the connection pool a definition names. Open answers a fresh pool whose ownership transfers to the caller: the registry closes every database it decides not to keep — one handed back beside an error, one that loses a duplicate migration open, one that lands after Close — so an implementation handing out a shared or memoized *bun.DB would have its live pool closed underneath it. */
type Provider interface {
    Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}

/* ContextOpener is the optional capability of opening under a caller's context: the retry loop between attempts sleeps on the context as well as the clock, so a shutdown that cancels it reaches a dial in flight instead of sleeping through the whole retry budget — the exact window in which supervisors send their signals. The registry prefers it whenever the provider implements it, handing the context it was constructed with. */
type ContextOpener interface {
    OpenContext(ctx context.Context, params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}

/* SecretParameterProvider is the optional capability of naming the configuration parameters that hold this provider's credentials. This major hands the connection values to the provider rather than the names it would read them under, so the application is ordinarily the party that knows the keys and names them to MarkSecretParameters; a provider that does know its own — one built around parameter names of its own — declares them here and is asked by the same door. A provider without the capability is not asked and is unaffected. */
type SecretParameterProvider interface {
    SecretParameterNames() []string
}

/* MigrationProvider is the optional capability of opening a connection tuned for migrations: the pool a provider opens for request traffic carries driver-level read and write deadlines sized for requests, and a legitimate DDL statement that runs past them — an ALTER TABLE adding constraints on a large table — is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. A provider that implements this opens the same database with those deadlines lifted; the migration commands prefer it and fall back to the ordinary connection when the capability is absent. */
type MigrationProvider interface {
    OpenForMigration(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}

/* MigrationContextOpener is what ContextOpener is to Open: the migration open under the caller's context, so the context the registry was constructed with reaches this door too. Without it the registry's promise held on the Manager path alone — the migration open took no context at all — and a db:migrate that received SIGTERM against a down database slept through the whole retry budget instead of refusing at the first cancellable step, which is the exact window a supervisor's signal lands in. The registry prefers it whenever the provider implements it; a provider carrying only MigrationProvider is unaffected. */
type MigrationContextOpener interface {
    OpenForMigrationContext(ctx context.Context, params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}
