package outbox

import (
    "context"
    "database/sql"
    "os"
    "testing"
    "time"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
)

func outboxTestStore(t *testing.T) *Store {
    t.Helper()

    dsn := os.Getenv("POSTGRES_DSN")
    if "" == dsn {
        t.Skip("POSTGRES_DSN not set; skipping outbox store integration test")
    }

    sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
    t.Cleanup(func() {
        sqldb.Close()
    })

    database := bun.NewDB(sqldb, pgdialect.New())

    store := NewStore(database, &stringCodec{})

    ctx := context.Background()
    if schemaErr := store.EnsureSchema(ctx); nil != schemaErr {
        t.Fatalf("ensure schema: %v", schemaErr)
    }

    /* start from a clean table so counts are deterministic across runs */
    if _, deleteErr := database.NewDelete().Model((*Message)(nil)).Where("1 = 1").Exec(ctx); nil != deleteErr {
        t.Fatalf("clear table: %v", deleteErr)
    }

    return store
}

func enqueueOutboxRows(t *testing.T, store *Store, count int) {
    t.Helper()

    ctx := context.Background()
    for index := 0; index < count; index++ {
        if enqueueErr := store.Enqueue(ctx, store.database, "payload"); nil != enqueueErr {
            t.Fatalf("enqueue: %v", enqueueErr)
        }
    }
}

func TestStore_ClaimMarksInFlightAndHidesFromNextClaim(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 3)

    claimed, claimErr := store.ClaimDueMessages(ctx, 10, time.Minute)
    if nil != claimErr {
        t.Fatalf("claim: %v", claimErr)
    }
    if 3 != len(claimed) {
        t.Fatalf("expected to claim all three due rows, got %d", len(claimed))
    }

    /* the same rows are now in-flight with a future visibility, so an immediate second claim sees nothing */
    again, againErr := store.ClaimDueMessages(ctx, 10, time.Minute)
    if nil != againErr {
        t.Fatalf("second claim: %v", againErr)
    }
    if 0 != len(again) {
        t.Fatalf("expected claimed rows to be hidden from the next claim, got %d", len(again))
    }
}

/* the heart of the no-Locker double-publish fix: a row another transaction is already working (holding a row lock on) must be skipped by a concurrent claim, never handed out a second time and never blocked on. Holding the lock in an open transaction makes the contention deterministic — without FOR UPDATE SKIP LOCKED the claim would either block on the held row (and hit the deadline) or hand it out again. */
func TestStore_ClaimSkipsRowLockedByAnotherTransaction(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    const rowCount = 5
    enqueueOutboxRows(t, store, rowCount)

    var lowest Message
    if scanErr := store.database.NewSelect().Model(&lowest).Order("id ASC").Limit(1).Scan(ctx); nil != scanErr {
        t.Fatalf("find lowest id: %v", scanErr)
    }

    /* hold a row lock on the lowest id in an open transaction, simulating another instance mid-claim */
    holdTx, beginErr := store.database.BeginTx(ctx, nil)
    if nil != beginErr {
        t.Fatalf("begin holding tx: %v", beginErr)
    }
    defer holdTx.Rollback()

    var held Message
    if lockErr := holdTx.NewSelect().Model(&held).Where("id = ?", lowest.Id).For("UPDATE").Scan(ctx); nil != lockErr {
        t.Fatalf("lock row: %v", lockErr)
    }

    /* bound the claim so a blocking (non-skip-locked) implementation fails on the deadline instead of hanging */
    claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    claimed, claimErr := store.ClaimDueMessages(claimCtx, rowCount, time.Minute)
    if nil != claimErr {
        t.Fatalf("claim blocked or errored; a held row must be skipped, not waited on: %v", claimErr)
    }

    for _, pending := range claimed {
        if pending.Id == lowest.Id {
            t.Fatalf("claimed row %d while another transaction held its lock", lowest.Id)
        }
    }

    if rowCount-1 != len(claimed) {
        t.Fatalf("expected to claim every row but the locked one (%d), got %d", rowCount-1, len(claimed))
    }
}

/* a stale run whose claim has lapsed must not clobber a row another instance already resolved: the resolution writes are guarded on status = in-flight, so a Reschedule arriving after the row was marked sent is a harmless no-op rather than reviving a delivered message. */
func TestStore_StaleResolveDoesNotClobberResolvedRow(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    claimed, claimErr := store.ClaimDueMessages(ctx, 10, time.Minute)
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }
    id := claimed[0].Id

    /* the current owner resolves the row as sent */
    if sentErr := store.MarkSent(ctx, id); nil != sentErr {
        t.Fatalf("mark sent: %v", sentErr)
    }

    /* a stale run (its claim long lapsed) tries to reschedule the same id; the in-flight guard must make it a no-op */
    if rescheduleErr := store.Reschedule(ctx, id, 1, time.Now(), "stale"); nil != rescheduleErr {
        t.Fatalf("stale reschedule: %v", rescheduleErr)
    }

    var row Message
    if scanErr := store.database.NewSelect().Model(&row).Where("id = ?", id).Scan(ctx); nil != scanErr {
        t.Fatalf("reload row: %v", scanErr)
    }

    if StatusSent != row.Status {
        t.Fatalf("expected the row to stay sent despite the stale reschedule, got %q", row.Status)
    }
}

/* a row claimed by an instance that crashed before resolving it must become claimable again once its visibility timeout lapses. */
func TestStore_InFlightRowResurfacesAfterVisibility(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    first, firstErr := store.ClaimDueMessages(ctx, 10, 50*time.Millisecond)
    if nil != firstErr {
        t.Fatalf("first claim: %v", firstErr)
    }
    if 1 != len(first) {
        t.Fatalf("expected to claim the row, got %d", len(first))
    }

    time.Sleep(120 * time.Millisecond)

    second, secondErr := store.ClaimDueMessages(ctx, 10, time.Minute)
    if nil != secondErr {
        t.Fatalf("second claim: %v", secondErr)
    }
    if 1 != len(second) || first[0].Id != second[0].Id {
        t.Fatalf("expected the in-flight row to re-surface after its visibility lapsed, got %+v", second)
    }
}
