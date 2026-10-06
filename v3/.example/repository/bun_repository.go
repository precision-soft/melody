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
    "github.com/precision-soft/melody/v3/.example/migration"
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


/* seedIfEmptyAudited fills an empty table of an audited entity in one transaction, each row inserted and its insert entry recorded through it, so a seed that fails half way leaves no partial nomenclature and no entry behind; the entries keep the audit transaction the context names when a reset opened one. Several processes may reach an empty table at once: the one whose insert the primary key refuses lost to another seed, so it rolls back whole and reads the count again, and a table another process seeded is left as it is. */
func seedIfEmptyAudited[Row any](ctx context.Context, database *bun.DB, recorder *melodyaudit.Recorder, auditEntity string, buildRows func() []*Row, idOf func(row *Row) string) error {
    for attempt := 0; ; attempt++ {
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

        seedErr := database.RunInTx(auditContext(ctx), nil, func(ctx context.Context, tx bun.Tx) error {
            for _, row := range buildRows() {
                if _, insertErr := tx.NewInsert().Model(row).Exec(ctx); nil != insertErr {
                    return insertErr
                }

                if recordErr := recorder.RecordInsert(melodyaudit.WithDatabase(ctx, tx), auditEntity, idOf(row), row); nil != recordErr {
                    return recordErr
                }
            }

            return nil
        })

        if 0 == attempt && true == errors.Is(asIdAlreadyExists(seedErr), ErrIdAlreadyExists) {
            continue
        }

        return seedErr
    }
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

/* identifierSequence is what a table's creates keep in the identifier sequence: the prefix its minted identifiers carry, and the identifier a create ended with. */
type identifierSequence struct {
    prefix     string
    identifier func() string
}

/* insertWithMintedIdentifier mints and inserts under the table's MySQL advisory lock, taken by one create of this process at a time, so two creates never read the same highest identifier: unserialized, every create read the list before any committed, minted the same one, and the primary key refused all but one. GET_LOCK waits on the server and belongs to the session that took it, so the lock is taken and released on one connection pinned for the call, while the mint and the insert run on the handle's pool; a release that cannot be issued ends the session, which releases the lock. A caller-supplied identifier is inserted without the lock.

   The mint is handed the floor the sequence keeps, the highest identifier ever stored under the prefix, and every identifier raises it before it is stored, the seeded ones included: the highest identifier present is not enough, since deleting the newest entity would hand its identifier, and the history and references that still name it, to the next one. */
func insertWithMintedIdentifier(ctx context.Context, database *bun.DB, lockName string, sequence identifierSequence, mintsIdentifier bool, mint func(floor string) error, insert func() error) error {
    /* the sequence is raised BEFORE the insert, on both paths: the insert commits in a transaction of its own, so a raise after it could fail over a stored row, answering an error for an entity that exists and leaving the floor below it. Raised first, a failed insert costs one unused number, and GREATEST keeps the floor from going back. */
    if false == mintsIdentifier {
        if recordErr := recordStoredIdentifier(ctx, database, sequence.prefix, sequence.identifier()); nil != recordErr {
            return recordErr
        }

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

    /* a supplied identifier is inserted without the lock, so it can take the one the mint chose between the read and the insert; the mint is made again past the floor that create raised */
    for attempt := 0; attempt < identifierMintAttempts; attempt++ {
        floor, floorErr := mintFloor(ctx, database, sequence.prefix)
        if nil != floorErr {
            return floorErr
        }

        if mintErr := mint(floor); nil != mintErr {
            return mintErr
        }

        if recordErr := recordStoredIdentifier(ctx, database, sequence.prefix, sequence.identifier()); nil != recordErr {
            return recordErr
        }

        insertErr := insert()
        if false == errors.Is(asIdAlreadyExists(insertErr), ErrIdAlreadyExists) {
            return insertErr
        }
    }

    return fmt.Errorf("the identifier mint under %s lost to a supplied identifier %d times", lockName, identifierMintAttempts)
}

/* identifierMintAttempts bounds how many times a create mints again after a supplied identifier took the one it minted */
const identifierMintAttempts = 3

/* mintFloor answers the identifier the sequence names as the highest ever stored under the prefix, or "" before the first */
func mintFloor(ctx context.Context, database bun.IDB, prefix string) (string, error) {
    highest := make([]int64, 0, 1)

    selectErr := database.NewSelect().
        Table(migration.IdentifierSequenceTableName).
        Column("highest_suffix").
        Where("entity = ?", prefix).
        Scan(ctx, &highest)
    if nil != selectErr {
        return "", selectErr
    }

    if 0 == len(highest) {
        return "", nil
    }

    return fmt.Sprintf("%s%d", prefix, highest[0]), nil
}

/* recordStoredIdentifier raises the prefix's sequence to the identifier's numeric tail, never lowers it; an identifier without one, a seeded cur-eur, leaves it as it is */
func recordStoredIdentifier(ctx context.Context, database bun.IDB, prefix string, identifier string) error {
    suffix := highestIdSuffix([]string{identifier}, prefix)
    if 0 == suffix {
        return nil
    }

    _, execErr := database.NewRaw(
        "INSERT INTO ? (entity, highest_suffix) VALUES (?, ?) ON DUPLICATE KEY UPDATE highest_suffix = GREATEST(highest_suffix, VALUES(highest_suffix))",
        bun.Ident(migration.IdentifierSequenceTableName),
        prefix,
        suffix,
    ).Exec(ctx)

    return execErr
}

/* raiseSequenceOverSeeds raises the sequence to the highest identifier of the seed under the prefix, on every resolution and ahead of the seed itself, so deleting a seeded entity never hands its identifier to the next create; a volume seeded before the sequence held its floor receives it the same way, and GREATEST makes the raise a no-op once it holds. */
func raiseSequenceOverSeeds(ctx context.Context, database bun.IDB, prefix string, identifierList []string) error {
    floor := seededFloor(identifierList, prefix)
    if "" == floor {
        return nil
    }

    return recordStoredIdentifier(ctx, database, prefix, floor)
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
