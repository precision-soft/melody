package repository

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "sync"
    "time"

    melodyaudit "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    bun "github.com/uptrace/bun"
)

/* affectedAtLeastOneRow answers whether a statement changed anything. A driver that does not report the count is read as "nothing was changed": the caller tells a write that landed from one that found no row, and a change nobody can confirm is not claimed. */
func affectedAtLeastOneRow(result sql.Result) bool {
    if nil == result {
        return false
    }

    affected, affectedErr := result.RowsAffected()
    if nil != affectedErr {
        return false
    }

    return 0 < affected
}

/* seedIfEmptyRows fills an empty table the constructor's migration set has already created, from the rows the caller builds. The table is read through the row type itself, so a caller cannot count one table and insert into another, and the insert ignores duplicate keys because several applications may reach an empty table at once. */
func seedIfEmptyRows[Row any](ctx context.Context, database *bun.DB, buildRows func() []*Row) error {
    count, countErr := database.
        NewSelect().
        Model((*Row)(nil)).
        Count(ctx)
    if nil != countErr {
        return countErr
    }

    if 0 < count {
        return nil
    }

    rowList := buildRows()

    _, insertErr := database.
        NewInsert().
        Model(&rowList).
        Ignore().
        Exec(ctx)

    return insertErr
}


/* seedIfEmptyAudited fills an empty table of an audited entity row by row through the audit tracker, so every seeded row carries its insert entry, joined to the audit transaction the context names when a reset opened one. A row another process seeded first is skipped, as the bulk seed's ignored duplicate is, since several processes may reach an empty table at once. */
func seedIfEmptyAudited[Row any](ctx context.Context, database *bun.DB, tracker *melodyaudit.Tracker, auditEntity string, buildRows func() []*Row, idOf func(row *Row) string) error {
    count, countErr := database.
        NewSelect().
        Model((*Row)(nil)).
        Count(ctx)
    if nil != countErr {
        return countErr
    }

    if 0 < count {
        return nil
    }

    for _, row := range buildRows() {
        insertErr := tracker.Insert(auditContext(ctx), auditEntity, idOf(row), row)
        if true == errors.Is(asIdAlreadyExists(insertErr), ErrIdAlreadyExists) {
            continue
        }

        if nil != insertErr {
            return insertErr
        }
    }

    return nil
}

/* ErrIdAlreadyExists is the refusal a create answers for a supplied identifier another row holds, whether the read before the insert or the primary key caught it, so the http doors answer 409 rather than 500. */
var ErrIdAlreadyExists = errors.New("id already exists")

/* asIdAlreadyExists maps the primary key's refusal of a supplied identifier onto ErrIdAlreadyExists: the read before the insert cannot stop a concurrent create of the same identifier, and the key is what holds it. The refusal is read from its key clause down the whole chain of causes; any other failure is answered untouched. */
func asIdAlreadyExists(insertErr error) error {
    if nil == insertErr {
        return nil
    }

    if false == errorChainNamesKey(insertErr, "PRIMARY") {
        return insertErr
    }

    return ErrIdAlreadyExists
}

/* identifierMintLockWait bounds how long a create waits, on the server, for another create of the same table to finish minting and inserting */
const identifierMintLockWait = 10 * time.Second

/* identifierMintLockReleaseTimeout bounds the release, issued on a fresh context so a request that ended cannot leave the lock held on a pooled connection */
const identifierMintLockReleaseTimeout = 5 * time.Second

/* insertWithMintedIdentifier mints and inserts under the table's MySQL advisory lock, taken by one create of this process at a time, so two creates never read the same highest identifier: unserialized, every create read the list before any committed, minted the same one, and the primary key refused all but one. GET_LOCK waits on the server and belongs to the session that took it, so the lock is taken and released on one connection pinned for the call, while the mint and the insert run on the handle's pool; a release that cannot be issued ends the session, which releases the lock. A caller-supplied identifier is inserted without the lock. */
func insertWithMintedIdentifier(ctx context.Context, database *bun.DB, lockName string, mintsIdentifier bool, mint func() error, insert func() error) error {
    if false == mintsIdentifier {
        return asIdAlreadyExists(insert())
    }

    /* the creates of this process queue here, holding no connection: waiting in GET_LOCK holds a pooled connection, so twenty waiters on a pool of ten would leave the holder none for its read and its insert */
    gate := identifierMintGate(lockName)
    select {
    case gate <- struct{}{}:
    case <-ctx.Done():
        return ctx.Err()
    }
    defer func() {
        <-gate
    }()

    connection, connectionErr := database.DB.Conn(ctx)
    if nil != connectionErr {
        return connectionErr
    }

    var acquired sql.NullInt64
    lockErr := connection.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", lockName, int64(identifierMintLockWait/time.Second)).Scan(&acquired)
    if nil != lockErr {
        /* GET_LOCK may have taken the lock on the server before the answer was lost */
        releaseIdentifierMintLock(connection, lockName)

        return lockErr
    }

    if false == acquired.Valid || 1 != acquired.Int64 {
        _ = connection.Close()

        return fmt.Errorf("the identifier mint lock %s was not taken within %s", lockName, identifierMintLockWait)
    }

    defer releaseIdentifierMintLock(connection, lockName)

    if mintErr := mint(); nil != mintErr {
        return mintErr
    }

    return insert()
}

/* identifierMintGates holds one single-slot gate per lock name, so the creates of one process take the advisory lock one at a time */
var identifierMintGates sync.Map

func identifierMintGate(lockName string) chan struct{} {
    gate, _ := identifierMintGates.LoadOrStore(lockName, make(chan struct{}, 1))

    return gate.(chan struct{})
}

func releaseIdentifierMintLock(connection *sql.Conn, lockName string) {
    releaseCtx, cancel := context.WithTimeout(context.Background(), identifierMintLockReleaseTimeout)
    defer cancel()

    if _, releaseErr := connection.ExecContext(releaseCtx, "DO RELEASE_LOCK(?)", lockName); nil != releaseErr {
        _ = connection.Raw(func(_ any) error {
            return driver.ErrBadConn
        })
    }

    _ = connection.Close()
}
