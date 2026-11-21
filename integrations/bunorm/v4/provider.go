package bunorm

import (
    "github.com/uptrace/bun"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* Provider opens the connection pools a definition names, one door per purpose, each under the runtime it is handed: the registry hands the caller's runtime over a context its own Close also ends, so a retry loop that waits on the context as well as the clock stops when the caller leaves or the registry closes, instead of sleeping through the whole retry budget. A nil runtime, or one carrying no context, is refused by the door.

   Open answers a fresh pool for request traffic whose ownership transfers to the caller: the registry closes every database it decides not to keep — one handed back beside an error, one that lands after Close — so an implementation handing out a shared or memoized *bun.DB from Open would have its live pool closed underneath it.

   OpenForMigration answers the connection the migration commands run on. The pool Open answers carries driver-level read and write deadlines sized for requests, and a legitimate DDL statement that runs past them — an ALTER TABLE adding constraints on a large table — is cut mid-statement, outside any transaction the database would roll back; a provider with such deadlines opens the same database with them lifted. A provider with nothing to tune answers Open's pool: func (instance *P) OpenForMigration(runtimeInstance, parameters, logger) (*bun.DB, error) { return instance.Open(runtimeInstance, parameters, logger) }. The registry keeps what it answers as the name's migration connection, shared by every caller and closed with the last CloseMigrationDatabase; an answer that is the very *bun.DB the registry already holds as the name's pool, compared by identity, is shared with that pool instead, neither kept as a migration connection nor closed by CloseMigrationDatabase. */
type Provider interface {
    Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)

    OpenForMigration(runtimeInstance containercontract.Runtime, parameters ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}
