package outbox

import (
    "context"
    "database/sql"
    "math"
    "os"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
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

    again, againErr := store.ClaimDueMessages(ctx, 10, time.Minute)
    if nil != againErr {
        t.Fatalf("second claim: %v", againErr)
    }
    if 0 != len(again) {
        t.Fatalf("expected claimed rows to be hidden from the next claim, got %d", len(again))
    }
}

func TestStore_ClaimSkipsRowLockedByAnotherTransaction(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    const rowCount = 5
    enqueueOutboxRows(t, store, rowCount)

    var lowest Message
    if scanErr := store.database.NewSelect().Model(&lowest).Order("id ASC").Limit(1).Scan(ctx); nil != scanErr {
        t.Fatalf("find lowest id: %v", scanErr)
    }

    holdTx, beginErr := store.database.BeginTx(ctx, nil)
    if nil != beginErr {
        t.Fatalf("begin holding tx: %v", beginErr)
    }
    defer holdTx.Rollback()

    var held Message
    if lockErr := holdTx.NewSelect().Model(&held).Where("id = ?", lowest.Id).For("UPDATE").Scan(ctx); nil != lockErr {
        t.Fatalf("lock row: %v", lockErr)
    }

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

func TestStore_StaleResolveDoesNotClobberResolvedRow(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    claimed, claimErr := store.ClaimDueMessages(ctx, 10, time.Minute)
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }
    id := claimed[0].Id

    if sentErr := store.MarkSent(ctx, id, claimed[0].ClaimToken); nil != sentErr {
        t.Fatalf("mark sent: %v", sentErr)
    }

    if rescheduleErr := store.Reschedule(ctx, id, 1, time.Now(), "stale", "stale-claim-token"); nil != rescheduleErr {
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

func TestStore_RecordDeliveryAttemptIncrementsPerRowNotClaim(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    claimed, claimErr := store.ClaimDueMessages(ctx, 10, time.Minute)
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }
    if 0 != claimed[0].DeliveryAttempts {
        t.Fatalf("expected the claim to leave delivery_attempts at 0, got %d", claimed[0].DeliveryAttempts)
    }
    id := claimed[0].Id

    first, claimedOk, recordErr := store.RecordDeliveryAttempt(ctx, id, claimed[0].ClaimToken)
    if nil != recordErr {
        t.Fatalf("record delivery attempt: %v", recordErr)
    }
    if false == claimedOk || 1 != first {
        t.Fatalf("expected the first recorded attempt to return 1 and claimed, got %d claimed=%v", first, claimedOk)
    }

    second, _, secondErr := store.RecordDeliveryAttempt(ctx, id, claimed[0].ClaimToken)
    if nil != secondErr {
        t.Fatalf("second record: %v", secondErr)
    }
    if 2 != second {
        t.Fatalf("expected the second recorded attempt to return 2, got %d", second)
    }
}

func TestStore_RecordDeliveryAttemptReportsUnclaimedWhenNotInFlight(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    claimed, claimErr := store.ClaimDueMessages(ctx, 10, time.Minute)
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }
    id := claimed[0].Id

    if sentErr := store.MarkSent(ctx, id, claimed[0].ClaimToken); nil != sentErr {
        t.Fatalf("mark sent: %v", sentErr)
    }

    _, claimedOk, recordErr := store.RecordDeliveryAttempt(ctx, id, claimed[0].ClaimToken)
    if nil != recordErr {
        t.Fatalf("record delivery attempt: %v", recordErr)
    }
    if true == claimedOk {
        t.Fatal("expected a resolved (no longer in-flight) row to report claimed=false")
    }
}

func TestStore_StaleClaimTokenCannotClobberReclaimedRow(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    firstClaim, firstErr := store.ClaimDueMessages(ctx, 10, 1*time.Millisecond)
    if nil != firstErr || 1 != len(firstClaim) {
        t.Fatalf("first claim: %v (got %d)", firstErr, len(firstClaim))
    }

    time.Sleep(10 * time.Millisecond)

    secondClaim, secondErr := store.ClaimDueMessages(ctx, 10, time.Minute)
    if nil != secondErr || 1 != len(secondClaim) {
        t.Fatalf("second claim: %v (got %d)", secondErr, len(secondClaim))
    }
    if firstClaim[0].ClaimToken == secondClaim[0].ClaimToken {
        t.Fatal("expected the re-claim to mint a distinct fencing token")
    }

    id := firstClaim[0].Id

    _, staleClaimed, staleRecordErr := store.RecordDeliveryAttempt(ctx, id, firstClaim[0].ClaimToken)
    if nil != staleRecordErr {
        t.Fatalf("stale record: %v", staleRecordErr)
    }
    if true == staleClaimed {
        t.Fatal("expected the stale claim's RecordDeliveryAttempt to report claimed=false after re-claim")
    }

    if sentErr := store.MarkSent(ctx, id, firstClaim[0].ClaimToken); nil != sentErr {
        t.Fatalf("stale mark sent: %v", sentErr)
    }

    _, freshClaimed, freshRecordErr := store.RecordDeliveryAttempt(ctx, id, secondClaim[0].ClaimToken)
    if nil != freshRecordErr {
        t.Fatalf("fresh record: %v", freshRecordErr)
    }
    if false == freshClaimed {
        t.Fatal("expected the current owner's RecordDeliveryAttempt to report claimed=true after a stale resolution")
    }
}

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

func TestClaimLimit_RefusesANonPositiveLimitAndCapsAboveTheMaximum(t *testing.T) {
    for _, refused := range []int{0, -1, math.MinInt} {
        if _, limitErr := claimLimit(refused); nil == limitErr {
            t.Fatalf("expected %d refused", refused)
        }
    }

    for input, expected := range map[int]int{1: 1, 1024: 1024, maximumBatchSize: maximumBatchSize, maximumBatchSize + 1: maximumBatchSize, 1 << 40: maximumBatchSize, math.MaxInt: maximumBatchSize} {
        limit, limitErr := claimLimit(input)
        if nil != limitErr {
            t.Fatalf("expected %d accepted, got %v", input, limitErr)
        }
        if expected != limit {
            t.Fatalf("expected %d to become %d, got %d", input, expected, limit)
        }
    }
}

func TestStore_ClaimDueMessagesRefusesANonPositiveLimitBeforeAnyQuery(t *testing.T) {
    sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN("postgres://nobody:nothing@127.0.0.1:1/nowhere?sslmode=disable")))
    t.Cleanup(func() {
        sqldb.Close()
    })

    store := NewStore(bun.NewDB(sqldb, pgdialect.New()), &stringCodec{})

    pending, claimErr := store.ClaimDueMessages(context.Background(), 0, time.Minute)
    if nil == claimErr || nil != pending {
        t.Fatalf("expected the zero limit refused before any query, got %v, %v", pending, claimErr)
    }
    if "outbox claim limit must be positive" != claimErr.Error() {
        t.Fatalf("expected the refusal by name, got %v", claimErr)
    }
    if 0 != exception.LogContext(claimErr)["limit"] {
        t.Fatalf("expected the refused limit in the context, got %v", exception.LogContext(claimErr))
    }
}
