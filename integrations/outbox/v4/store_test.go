package outbox

import (
    "context"
    "database/sql"
    "errors"
    "math"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/messagebus"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
)

func outboxTestStore(t *testing.T) *Store {
    t.Helper()

    return outboxTestStoreWith(t, StoreConfig{})
}

func outboxTestStoreWith(t *testing.T, config StoreConfig) *Store {
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

    store := NewStore(database, &stringCodec{}, config)

    ctx := context.Background()
    if schemaErr := store.EnsureSchema(outboxTestRuntime(ctx)); nil != schemaErr {
        t.Fatalf("ensure schema: %v", schemaErr)
    }

    /* start from a clean table so counts are deterministic across runs */
    if _, deleteErr := database.NewDelete().Model((*Message)(nil)).ModelTableExpr(aliasedTableExpression, bun.Ident(store.TableName())).Where("1 = 1").Exec(ctx); nil != deleteErr {
        t.Fatalf("clear table: %v", deleteErr)
    }

    return store
}

func enqueueOutboxRows(t *testing.T, store *Store, count int) {
    t.Helper()

    ctx := context.Background()
    for index := 0; index < count; index++ {
        if enqueueErr := store.Enqueue(ctx, store.database, "payload", EnqueueOptions{}); nil != enqueueErr {
            t.Fatalf("enqueue: %v", enqueueErr)
        }
    }
}

func TestStore_ClaimMarksInFlightAndHidesFromNextClaim(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 3)

    claimed, claimErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != claimErr {
        t.Fatalf("claim: %v", claimErr)
    }
    if 3 != len(claimed) {
        t.Fatalf("expected to claim all three due rows, got %d", len(claimed))
    }

    /* the same rows are now in-flight with a future visibility, so an immediate second claim sees nothing */
    again, againErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
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

    claimed, claimErr := store.ClaimDueMessages(outboxTestRuntime(claimCtx), rowCount, time.Minute, ClaimOptions{})
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

    claimed, claimErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }
    id := claimed[0].Id

    /* the current owner resolves the row as sent */
    if sentErr := store.MarkSent(outboxTestRuntime(ctx), id, claimed[0].ClaimToken); nil != sentErr {
        t.Fatalf("mark sent: %v", sentErr)
    }

    /* a stale run (its claim long lapsed) tries to reschedule the same id under its old fencing token; both the in-flight guard and the token guard must make it a no-op */
    if rescheduleErr := store.Reschedule(outboxTestRuntime(ctx), id, 1, time.Now(), "stale", "stale-claim-token"); nil != rescheduleErr {
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

/* claiming must NOT touch delivery_attempts; the relay charges an attempt per row at delivery time via RecordDeliveryAttempt, and the post-increment count drives the crash-poison cap. Charging per delivery (not per batch claim) keeps a batch-mate the relay never reached from being falsely climbed toward the poison cap. */
func TestStore_RecordDeliveryAttemptIncrementsPerRowNotClaim(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    claimed, claimErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }
    if 0 != claimed[0].DeliveryAttempts {
        t.Fatalf("expected the claim to leave delivery_attempts at 0, got %d", claimed[0].DeliveryAttempts)
    }
    id := claimed[0].Id

    first, claimedOk, recordErr := store.RecordDeliveryAttempt(outboxTestRuntime(ctx), id, claimed[0].ClaimToken)
    if nil != recordErr {
        t.Fatalf("record delivery attempt: %v", recordErr)
    }
    if false == claimedOk || 1 != first {
        t.Fatalf("expected the first recorded attempt to return 1 and claimed, got %d claimed=%v", first, claimedOk)
    }

    second, _, secondErr := store.RecordDeliveryAttempt(outboxTestRuntime(ctx), id, claimed[0].ClaimToken)
    if nil != secondErr {
        t.Fatalf("second record: %v", secondErr)
    }
    if 2 != second {
        t.Fatalf("expected the second recorded attempt to return 2, got %d", second)
    }
}

/* a row that is not in-flight (already resolved, or its claim lapsed and another instance owns it) must report claimed=false so the relay skips it instead of publishing alongside the new owner. */
func TestStore_RecordDeliveryAttemptReportsUnclaimedWhenNotInFlight(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    claimed, claimErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }
    id := claimed[0].Id

    if sentErr := store.MarkSent(outboxTestRuntime(ctx), id, claimed[0].ClaimToken); nil != sentErr {
        t.Fatalf("mark sent: %v", sentErr)
    }

    _, claimedOk, recordErr := store.RecordDeliveryAttempt(outboxTestRuntime(ctx), id, claimed[0].ClaimToken)
    if nil != recordErr {
        t.Fatalf("record delivery attempt: %v", recordErr)
    }
    if true == claimedOk {
        t.Fatal("expected a resolved (no longer in-flight) row to report claimed=false")
    }
}

/* the fencing token stops a stale run whose claim lapsed from clobbering a row another instance has re-claimed: once a re-claim rewrites claim_token, the original claim's RecordDeliveryAttempt/MarkSent must no-op while the current owner's still transition the row. */
func TestStore_StaleClaimTokenCannotClobberReclaimedRow(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    firstClaim, firstErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, 1*time.Millisecond, ClaimOptions{})
    if nil != firstErr || 1 != len(firstClaim) {
        t.Fatalf("first claim: %v (got %d)", firstErr, len(firstClaim))
    }

    /* let the visibility window lapse so the row is due again, then re-claim it under a fresh token */
    time.Sleep(10 * time.Millisecond)

    secondClaim, secondErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != secondErr || 1 != len(secondClaim) {
        t.Fatalf("second claim: %v (got %d)", secondErr, len(secondClaim))
    }
    if firstClaim[0].ClaimToken == secondClaim[0].ClaimToken {
        t.Fatal("expected the re-claim to mint a distinct fencing token")
    }

    id := firstClaim[0].Id

    _, staleClaimed, staleRecordErr := store.RecordDeliveryAttempt(outboxTestRuntime(ctx), id, firstClaim[0].ClaimToken)
    if nil != staleRecordErr {
        t.Fatalf("stale record: %v", staleRecordErr)
    }
    if true == staleClaimed {
        t.Fatal("expected the stale claim's RecordDeliveryAttempt to report claimed=false after re-claim")
    }

    /* a stale resolution must not clobber the new owner: this MarkSent matches no row */
    if sentErr := store.MarkSent(outboxTestRuntime(ctx), id, firstClaim[0].ClaimToken); nil != sentErr {
        t.Fatalf("stale mark sent: %v", sentErr)
    }

    _, freshClaimed, freshRecordErr := store.RecordDeliveryAttempt(outboxTestRuntime(ctx), id, secondClaim[0].ClaimToken)
    if nil != freshRecordErr {
        t.Fatalf("fresh record: %v", freshRecordErr)
    }
    if false == freshClaimed {
        t.Fatal("expected the current owner's RecordDeliveryAttempt to report claimed=true after a stale resolution")
    }
}

/* a row claimed by an instance that crashed before resolving it must become claimable again once its visibility timeout lapses. */
func TestStore_InFlightRowResurfacesAfterVisibility(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    first, firstErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, 50*time.Millisecond, ClaimOptions{})
    if nil != firstErr {
        t.Fatalf("first claim: %v", firstErr)
    }
    if 1 != len(first) {
        t.Fatalf("expected to claim the row, got %d", len(first))
    }

    time.Sleep(120 * time.Millisecond)

    second, secondErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != secondErr {
        t.Fatalf("second claim: %v", secondErr)
    }
    if 1 != len(second) || first[0].Id != second[0].Id {
        t.Fatalf("expected the in-flight row to re-surface after its visibility lapsed, got %+v", second)
    }
}

/* bun writes no LIMIT clause for a non-positive limit and narrows the value to int32 first, so zero, a negative and a value past the int32 range all claimed the whole table through this public door; a value above the relay's ceiling is refused too, the answer the relay's BatchSize gets. */
func TestClaimLimit_RefusesANonPositiveLimitAndOneAboveTheMaximum(t *testing.T) {
    for _, refused := range []int{0, -1, math.MinInt, maximumBatchSize + 1, 1 << 40, math.MaxInt} {
        if _, limitErr := claimLimit(refused); nil == limitErr {
            t.Fatalf("expected %d refused", refused)
        }
    }

    for input, expected := range map[int]int{1: 1, 1024: 1024, maximumBatchSize: maximumBatchSize} {
        limit, limitErr := claimLimit(input)
        if nil != limitErr {
            t.Fatalf("expected %d accepted, got %v", input, limitErr)
        }
        if expected != limit {
            t.Fatalf("expected %d to become %d, got %d", input, expected, limit)
        }
    }
}

/* the refusal precedes every query: the store below is built over a connector nothing can reach, so a query would have failed with a dial error rather than the refusal asserted here. */
func TestStore_ClaimDueMessagesRefusesANonPositiveLimitBeforeAnyQuery(t *testing.T) {
    sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN("postgres://nobody:nothing@127.0.0.1:1/nowhere?sslmode=disable")))
    t.Cleanup(func() {
        sqldb.Close()
    })

    store := NewStore(bun.NewDB(sqldb, pgdialect.New()), &stringCodec{}, StoreConfig{})

    pending, claimErr := store.ClaimDueMessages(outboxTestRuntime(context.Background()), 0, time.Minute, ClaimOptions{})
    if nil == claimErr || nil != pending {
        t.Fatalf("expected the zero limit refused before any query, got %v, %v", pending, claimErr)
    }
    if "outbox claim limit must be positive" != claimErr.Error() {
        t.Fatalf("expected the refusal by name, got %v", claimErr)
    }
    if 0 != exception.LogContext(claimErr, exception.LogContextOptions{})["limit"] {
        t.Fatalf("expected the refused limit in the context, got %v", exception.LogContext(claimErr, exception.LogContextOptions{}))
    }
}

/* claimQueryHook keeps the text of every query the store composes */
type claimQueryHook struct {
    queries []string
}

func (instance *claimQueryHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
    return ctx
}

func (instance *claimQueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
    instance.queries = append(instance.queries, event.Query)
}

func TestStore_AnOversizedClaimLimitIsRefusedBeforeAnyQuery(t *testing.T) {
    store := outboxTestStore(t)
    hook := &claimQueryHook{}
    store.database.AddQueryHook(hook)

    if _, claimErr := store.ClaimDueMessages(outboxTestRuntime(context.Background()), math.MaxInt32, time.Minute, ClaimOptions{}); nil == claimErr {
        t.Fatalf("expected an oversized claim limit refused")
    }

    if 0 != len(hook.queries) {
        t.Fatalf("expected no query for a refused limit, got %v", hook.queries)
    }

    if _, claimErr := store.ClaimDueMessages(outboxTestRuntime(context.Background()), maximumBatchSize, time.Minute, ClaimOptions{}); nil != claimErr {
        t.Fatalf("claim: %v", claimErr)
    }

    for _, query := range hook.queries {
        if true == strings.Contains(query, "LIMIT 100000") {
            return
        }
    }

    t.Fatalf("expected the claim at the maximum composed with LIMIT 100000, got %v", hook.queries)
}

func TestStore_RescheduleUnchargedHandsTheRowBackAsNeverTried(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    claimed, claimErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }

    if _, _, recordErr := store.RecordDeliveryAttempt(outboxTestRuntime(ctx), claimed[0].Id, claimed[0].ClaimToken); nil != recordErr {
        t.Fatalf("record delivery attempt: %v", recordErr)
    }

    if handBackErr := store.RescheduleUncharged(outboxTestRuntime(ctx), claimed[0].Id, time.Now().Add(-time.Second), "broker unreachable", claimed[0].ClaimToken); nil != handBackErr {
        t.Fatalf("hand back: %v", handBackErr)
    }

    reclaimed, reclaimErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != reclaimErr || 1 != len(reclaimed) {
        t.Fatalf("expected the row pending and due again: %v (got %d)", reclaimErr, len(reclaimed))
    }

    if 0 != reclaimed[0].Attempts || 0 != reclaimed[0].DeliveryAttempts {
        t.Fatalf("expected neither counter charged, got attempts %d delivery attempts %d", reclaimed[0].Attempts, reclaimed[0].DeliveryAttempts)
    }

    if _, _, recordErr := store.RecordDeliveryAttempt(outboxTestRuntime(ctx), reclaimed[0].Id, reclaimed[0].ClaimToken); nil != recordErr {
        t.Fatalf("record the re-claim's delivery attempt: %v", recordErr)
    }

    if staleErr := store.RescheduleUncharged(outboxTestRuntime(ctx), reclaimed[0].Id, time.Now(), "stale", claimed[0].ClaimToken); nil != staleErr {
        t.Fatalf("a stale hand back answers no error: %v", staleErr)
    }

    again, _ := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if 0 != len(again) {
        t.Fatalf("expected the stale claim token to leave the re-claimed row in flight, got %d due", len(again))
    }
}

func TestStore_RescheduleUnchargedHandsBackARowWhoseDeliveryCountIsZero(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    enqueueOutboxRows(t, store, 1)

    claimed, claimErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != claimErr || 1 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }

    if handBackErr := store.RescheduleUncharged(outboxTestRuntime(ctx), claimed[0].Id, time.Now().Add(-time.Second), "broker unreachable", claimed[0].ClaimToken); nil != handBackErr {
        t.Fatalf("hand back: %v", handBackErr)
    }

    reclaimed, reclaimErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 10, time.Minute, ClaimOptions{})
    if nil != reclaimErr || 1 != len(reclaimed) {
        t.Fatalf("expected the row pending and due again with its count left at zero: %v (got %d)", reclaimErr, len(reclaimed))
    }

    if 0 != reclaimed[0].DeliveryAttempts {
        t.Fatalf("expected the delivery count floored at zero, got %d", reclaimed[0].DeliveryAttempts)
    }
}

func reloadOutboxRow(t *testing.T, store *Store, id int64) Message {
    t.Helper()

    var row Message
    if scanErr := store.database.NewSelect().Model(&row).Where("id = ?", id).Scan(context.Background()); nil != scanErr {
        t.Fatalf("reload row %d: %v", id, scanErr)
    }

    return row
}

func TestStore_ReleaseClaimsHandsBackOnlyTheNamedRowsStillClaimedUnderTheToken(t *testing.T) {
    store := outboxTestStore(t)
    runtimeInstance := outboxTestRuntime(context.Background())

    enqueueOutboxRows(t, store, 3)

    claimed, claimErr := store.ClaimDueMessages(runtimeInstance, 3, time.Hour, ClaimOptions{})
    if nil != claimErr || 3 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }

    token := claimed[0].ClaimToken
    if _, _, recordErr := store.RecordDeliveryAttempt(runtimeInstance, claimed[0].Id, token); nil != recordErr {
        t.Fatalf("record: %v", recordErr)
    }

    if sentErr := store.MarkSent(runtimeInstance, claimed[1].Id, token); nil != sentErr {
        t.Fatalf("mark sent: %v", sentErr)
    }

    enqueueOutboxRows(t, store, 1)
    other, otherErr := store.ClaimDueMessages(runtimeInstance, 1, time.Hour, ClaimOptions{})
    if nil != otherErr || 1 != len(other) {
        t.Fatalf("second claim: %v (got %d)", otherErr, len(other))
    }

    released, releaseErr := store.ReleaseClaims(runtimeInstance, []int64{claimed[0].Id, claimed[1].Id, other[0].Id}, token)
    if nil != releaseErr || 1 != released {
        t.Fatalf("expected the one named row still claimed under the token released, got %d: %v", released, releaseErr)
    }

    row := reloadOutboxRow(t, store, claimed[0].Id)
    if StatusPending != row.Status || row.AvailableAt.After(time.Now()) || 0 != row.Attempts {
        t.Fatalf("expected row %d pending, available now, attempts unchanged, got %+v", claimed[0].Id, row)
    }

    if StatusInFlight != reloadOutboxRow(t, store, claimed[2].Id).Status {
        t.Fatal("expected the row the release does not name kept claimed")
    }

    if 1 != reloadOutboxRow(t, store, claimed[0].Id).DeliveryAttempts {
        t.Fatal("expected the recorded delivery attempt kept")
    }

    if StatusSent != reloadOutboxRow(t, store, claimed[1].Id).Status {
        t.Fatal("expected the sent row untouched")
    }

    if StatusInFlight != reloadOutboxRow(t, store, other[0].Id).Status {
        t.Fatal("expected the row claimed under another token untouched")
    }
}

func markOutboxRowsDead(t *testing.T, store *Store, count int) []int64 {
    t.Helper()

    runtimeInstance := outboxTestRuntime(context.Background())
    enqueueOutboxRows(t, store, count)

    claimed, claimErr := store.ClaimDueMessages(runtimeInstance, count, time.Hour, ClaimOptions{})
    if nil != claimErr || count != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }

    ids := make([]int64, 0, count)
    for _, pending := range claimed {
        if _, _, recordErr := store.RecordDeliveryAttempt(runtimeInstance, pending.Id, pending.ClaimToken); nil != recordErr {
            t.Fatalf("record: %v", recordErr)
        }

        if deadErr := store.MarkDead(runtimeInstance, pending.Id, 3, "poison", pending.ClaimToken); nil != deadErr {
            t.Fatalf("mark dead: %v", deadErr)
        }

        ids = append(ids, pending.Id)
    }

    return ids
}

func TestStore_RequeueDeadReturnsTheSelectedDeadRowsWithTheirCountersReset(t *testing.T) {
    store := outboxTestStore(t)
    runtimeInstance := outboxTestRuntime(context.Background())

    dead := markOutboxRowsDead(t, store, 3)
    enqueueOutboxRows(t, store, 1)

    requeued, requeueErr := store.RequeueDead(runtimeInstance, RequeueDeadOptions{Ids: []int64{dead[1]}})
    if nil != requeueErr || 1 != requeued {
        t.Fatalf("expected the one named dead row requeued, got %d: %v", requeued, requeueErr)
    }

    row := reloadOutboxRow(t, store, dead[1])
    if StatusPending != row.Status || 0 != row.Attempts || 0 != row.DeliveryAttempts || "poison" != row.LastError || row.AvailableAt.After(time.Now()) {
        t.Fatalf("expected the row pending, counters reset, last_error kept, got %+v", row)
    }

    if StatusDead != reloadOutboxRow(t, store, dead[0]).Status {
        t.Fatal("expected the row not named left dead")
    }

    limited, limitedErr := store.RequeueDead(runtimeInstance, RequeueDeadOptions{Limit: 1})
    if nil != limitedErr || 1 != limited {
        t.Fatalf("expected the limit to bound the requeue to one row, got %d: %v", limited, limitedErr)
    }

    rest, restErr := store.RequeueDead(runtimeInstance, RequeueDeadOptions{})
    if nil != restErr || 1 != rest {
        t.Fatalf("expected the last dead row requeued, got %d: %v", rest, restErr)
    }

    if _, negativeErr := store.RequeueDead(runtimeInstance, RequeueDeadOptions{Limit: -1}); nil == negativeErr {
        t.Fatal("expected a negative limit refused")
    }
}

func TestStore_PruneSentDeletesOnlyRowsSentBeforeTheCutOff(t *testing.T) {
    store := outboxTestStore(t)
    runtimeInstance := outboxTestRuntime(context.Background())

    if _, zeroErr := store.PruneSent(runtimeInstance, PruneSentOptions{}); nil == zeroErr {
        t.Fatal("expected a zero cut-off refused")
    } else {
        expectErrorNamingField(t, zeroErr, "PruneSentOptions.SentBefore")
    }

    enqueueOutboxRows(t, store, 3)
    claimed, claimErr := store.ClaimDueMessages(runtimeInstance, 3, time.Hour, ClaimOptions{})
    if nil != claimErr || 3 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }

    for _, pending := range claimed[:2] {
        if sentErr := store.MarkSent(runtimeInstance, pending.Id, pending.ClaimToken); nil != sentErr {
            t.Fatalf("mark sent: %v", sentErr)
        }
    }

    /* the cut-off judges the send time: the first sent row was sent long ago, the second enqueued long ago and sent now, so only the first is pruned */
    old := time.Now().Add(-48 * time.Hour)
    if _, updateErr := store.database.NewUpdate().Model((*Message)(nil)).Set("sent_at = ?", old).Where("id = ?", claimed[0].Id).Exec(context.Background()); nil != updateErr {
        t.Fatalf("age the send: %v", updateErr)
    }

    if _, updateErr := store.database.NewUpdate().Model((*Message)(nil)).Set("created_at = ?", old).Where("id IN (?)", bun.In([]int64{claimed[1].Id, claimed[2].Id})).Exec(context.Background()); nil != updateErr {
        t.Fatalf("age rows: %v", updateErr)
    }

    dead := markOutboxRowsDead(t, store, 1)
    if _, updateErr := store.database.NewUpdate().Model((*Message)(nil)).Set("created_at = ?", old).Where("id = ?", dead[0]).Exec(context.Background()); nil != updateErr {
        t.Fatalf("age dead row: %v", updateErr)
    }

    pruned, pruneErr := store.PruneSent(runtimeInstance, PruneSentOptions{SentBefore: time.Now().Add(-24 * time.Hour)})
    if nil != pruneErr || 1 != pruned {
        t.Fatalf("expected the one old sent row pruned, got %d: %v", pruned, pruneErr)
    }

    remaining := 0
    countErr := store.database.NewSelect().Model((*Message)(nil)).ColumnExpr("count(*)").Where("id IN (?)", bun.In([]int64{claimed[0].Id, claimed[1].Id, claimed[2].Id, dead[0]})).Scan(context.Background(), &remaining)
    if nil != countErr || 3 != remaining {
        t.Fatalf("expected the row enqueued long ago and sent now, the old in-flight row and the old dead row kept, got %d: %v", remaining, countErr)
    }

    prunedRows := 0
    if prunedErr := store.database.NewSelect().Model((*Message)(nil)).ColumnExpr("count(*)").Where("id = ?", claimed[0].Id).Scan(context.Background(), &prunedRows); nil != prunedErr || 0 != prunedRows {
        t.Fatalf("expected the row sent long ago to be the one pruned, got %d: %v", prunedRows, prunedErr)
    }

    enqueueOutboxRows(t, store, 2)
    more, moreErr := store.ClaimDueMessages(runtimeInstance, 2, time.Hour, ClaimOptions{})
    if nil != moreErr || 2 != len(more) {
        t.Fatalf("claim: %v (got %d)", moreErr, len(more))
    }

    for _, pending := range more {
        if sentErr := store.MarkSent(runtimeInstance, pending.Id, pending.ClaimToken); nil != sentErr {
            t.Fatalf("mark sent: %v", sentErr)
        }
    }

    limited, limitedErr := store.PruneSent(runtimeInstance, PruneSentOptions{SentBefore: time.Now().Add(time.Hour), Limit: 1})
    if nil != limitedErr || 1 != limited {
        t.Fatalf("expected the limit to bound the prune to one row, got %d: %v", limited, limitedErr)
    }

    afterAnInFlightRow, afterErr := store.PruneSent(runtimeInstance, PruneSentOptions{SentBefore: time.Now().Add(time.Hour), Limit: 1})
    if nil != afterErr || 1 != afterAnInFlightRow {
        t.Fatalf("expected the limit to count sent rows only, an older in-flight row passed over, got %d: %v", afterAnInFlightRow, afterErr)
    }
}

func TestStore_EveryRepositoryDoorRefusesANilRuntime(t *testing.T) {
    store := &Store{}

    _, claimErr := store.ClaimDueMessages(nil, 1, time.Minute, ClaimOptions{})
    _, _, recordErr := store.RecordDeliveryAttempt(nil, 1, "token")
    _, releaseErr := store.ReleaseClaims(nil, []int64{1}, "token")
    _, requeueErr := store.RequeueDead(nil, RequeueDeadOptions{})
    _, pruneErr := store.PruneSent(nil, PruneSentOptions{SentBefore: time.Now()})

    for name, doorErr := range map[string]error{
        "ClaimDueMessages":      claimErr,
        "RecordDeliveryAttempt": recordErr,
        "MarkSent":              store.MarkSent(nil, 1, "token"),
        "Reschedule":            store.Reschedule(nil, 1, 1, time.Now(), "", "token"),
        "RescheduleUncharged":   store.RescheduleUncharged(nil, 1, time.Now(), "", "token"),
        "MarkDead":              store.MarkDead(nil, 1, 1, "", "token"),
        "ReleaseClaims":         releaseErr,
        "RequeueDead":           requeueErr,
        "PruneSent":             pruneErr,
    } {
        if nil == doorErr || false == strings.Contains(doorErr.Error(), "the runtime is nil or carries no context") {
            t.Fatalf("%s: expected a nil runtime refused, got %v", name, doorErr)
        }
    }
}

/* the refusals run before the store is touched: the store here has no database, so a refusal moved below the statement panics the test */
func TestStore_ReleaseClaimsRefusesAnEmptyTokenAndTooManyRowsAndReleasesNothingForNoRow(t *testing.T) {
    store := &Store{}
    runtimeInstance := outboxTestRuntime(context.Background())

    if _, releaseErr := store.ReleaseClaims(runtimeInstance, []int64{1}, ""); nil == releaseErr || false == strings.Contains(releaseErr.Error(), "outbox claim release token is empty") {
        t.Fatalf("expected an empty claim token refused, got %v", releaseErr)
    }

    if _, releaseErr := store.ReleaseClaims(runtimeInstance, make([]int64, maximumBatchSize+1), "token"); nil == releaseErr || false == strings.Contains(releaseErr.Error(), "more rows than the maximum batch size") {
        t.Fatalf("expected more ids than the maximum batch size refused, got %v", releaseErr)
    }

    if released, releaseErr := store.ReleaseClaims(runtimeInstance, nil, "token"); nil != releaseErr || 0 != released {
        t.Fatalf("expected no row named to release nothing, got %d: %v", released, releaseErr)
    }
}

/* more ids than the limit are refused rather than cut to it, before the store is touched */
func TestStore_RequeueDeadRefusesMoreIdsThanItsLimit(t *testing.T) {
    store := &Store{}

    _, requeueErr := store.RequeueDead(outboxTestRuntime(context.Background()), RequeueDeadOptions{Ids: []int64{1, 2, 3}, Limit: 2})
    if nil == requeueErr || false == strings.Contains(requeueErr.Error(), "outbox requeue names more rows than its limit") {
        t.Fatalf("expected more ids than the limit refused, got %v", requeueErr)
    }

    logContext := exception.LogContext(requeueErr, exception.LogContextOptions{})
    if "RequeueDeadOptions.Ids" != logContext["field"] {
        t.Fatalf("expected the refusal to name RequeueDeadOptions.Ids, got %v", logContext)
    }
}

func TestStore_MarkSentRecordsTheSendTimeAndARowWithNoSendTimeIsNeverPruned(t *testing.T) {
    store := outboxTestStore(t)
    runtimeInstance := outboxTestRuntime(context.Background())

    enqueueOutboxRows(t, store, 2)
    claimed, claimErr := store.ClaimDueMessages(runtimeInstance, 2, time.Hour, ClaimOptions{})
    if nil != claimErr || 2 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }

    before := time.Now().Add(-time.Second)
    for _, pending := range claimed {
        if sentErr := store.MarkSent(runtimeInstance, pending.Id, pending.ClaimToken); nil != sentErr {
            t.Fatalf("mark sent: %v", sentErr)
        }
    }

    row := new(Message)
    if selectErr := store.database.NewSelect().Model(row).Where("id = ?", claimed[0].Id).Scan(context.Background()); nil != selectErr {
        t.Fatalf("select: %v", selectErr)
    }

    if true == row.SentAt.IsZero() || true == row.SentAt.Before(before) || true == row.SentAt.After(time.Now().Add(time.Second)) {
        t.Fatalf("expected the send time recorded now, got %v", row.SentAt)
    }

    /* a row sent before the column existed carries no send time: it is kept, the upgrade guide backfilling it from created_at */
    if _, updateErr := store.database.NewUpdate().Model((*Message)(nil)).Set("sent_at = NULL").Where("id = ?", claimed[1].Id).Exec(context.Background()); nil != updateErr {
        t.Fatalf("clear the send time: %v", updateErr)
    }

    pruned, pruneErr := store.PruneSent(runtimeInstance, PruneSentOptions{SentBefore: time.Now().Add(time.Hour)})
    if nil != pruneErr || 1 != pruned {
        t.Fatalf("expected only the row with a send time pruned, got %d: %v", pruned, pruneErr)
    }
}

func TestStore_EnsureSchemaAddsTheSendTimeToATableCreatedWithoutIt(t *testing.T) {
    store := outboxTestStore(t)

    if _, dropErr := store.database.NewRaw("ALTER TABLE melody_outbox DROP COLUMN sent_at").Exec(context.Background()); nil != dropErr {
        t.Fatalf("drop the column: %v", dropErr)
    }

    if schemaErr := store.EnsureSchema(outboxTestRuntime(context.Background())); nil != schemaErr {
        t.Fatalf("ensure schema: %v", schemaErr)
    }

    if schemaErr := store.EnsureSchema(outboxTestRuntime(context.Background())); nil != schemaErr {
        t.Fatalf("ensure schema again: %v", schemaErr)
    }

    columns := 0
    if scanErr := store.database.NewRaw("SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'melody_outbox' AND column_name = 'sent_at'").Scan(context.Background(), &columns); nil != scanErr || 1 != columns {
        t.Fatalf("expected the column added once, got %d: %v", columns, scanErr)
    }
}

func outboxTableRowCount(t *testing.T, store *Store, tableName string) int {
    t.Helper()

    count := 0
    if scanErr := store.database.NewRaw("SELECT count(*) FROM ?", bun.Ident(tableName)).Scan(context.Background(), &count); nil != scanErr {
        t.Fatalf("count %s: %v", tableName, scanErr)
    }

    return count
}

func TestStore_TheTableNameIsTheConfigs(t *testing.T) {
    defaultStore := outboxTestStore(t)
    enqueueOutboxRows(t, defaultStore, 1)

    if _, dropErr := defaultStore.database.NewRaw("DROP TABLE IF EXISTS orders_outbox").Exec(context.Background()); nil != dropErr {
        t.Fatalf("drop: %v", dropErr)
    }

    store := outboxTestStoreWith(t, StoreConfig{TableName: "orders_outbox"})
    t.Cleanup(func() { _, _ = store.database.NewRaw("DROP TABLE IF EXISTS orders_outbox").Exec(context.Background()) })
    if "orders_outbox" != store.TableName() {
        t.Fatalf("expected the configured table name, got %q", store.TableName())
    }

    runtimeInstance := outboxTestRuntime(context.Background())
    enqueueOutboxRows(t, store, 5)

    claimed, claimErr := store.ClaimDueMessages(runtimeInstance, 5, time.Hour, ClaimOptions{})
    if nil != claimErr || 5 != len(claimed) {
        t.Fatalf("claim: %v (got %d)", claimErr, len(claimed))
    }

    for _, pending := range claimed {
        if "orders_outbox" != pending.Table {
            t.Fatalf("expected the claimed row to name its table, got %q", pending.Table)
        }

        if _, recorded, recordErr := store.RecordDeliveryAttempt(runtimeInstance, pending.Id, pending.ClaimToken); nil != recordErr || false == recorded {
            t.Fatalf("record: %v (recorded %v)", recordErr, recorded)
        }
    }

    for _, step := range []error{
        store.MarkSent(runtimeInstance, claimed[0].Id, claimed[0].ClaimToken),
        store.Reschedule(runtimeInstance, claimed[1].Id, 1, time.Now().Add(time.Hour), "retry", claimed[1].ClaimToken),
        store.RescheduleUncharged(runtimeInstance, claimed[2].Id, time.Now().Add(time.Hour), "outage", claimed[2].ClaimToken),
        store.MarkDead(runtimeInstance, claimed[3].Id, 3, "poison", claimed[3].ClaimToken),
    } {
        if nil != step {
            t.Fatalf("resolution: %v", step)
        }
    }

    if released, releaseErr := store.ReleaseClaims(runtimeInstance, []int64{claimed[4].Id}, claimed[4].ClaimToken); nil != releaseErr || 1 != released {
        t.Fatalf("release: %v (released %d)", releaseErr, released)
    }

    if requeued, requeueErr := store.RequeueDead(runtimeInstance, RequeueDeadOptions{}); nil != requeueErr || 1 != requeued {
        t.Fatalf("requeue dead: %v (requeued %d)", requeueErr, requeued)
    }

    if pruned, pruneErr := store.PruneSent(runtimeInstance, PruneSentOptions{SentBefore: time.Now().Add(time.Hour)}); nil != pruneErr || 1 != pruned {
        t.Fatalf("prune: %v (pruned %d)", pruneErr, pruned)
    }

    if 4 != outboxTableRowCount(t, store, "orders_outbox") {
        t.Fatalf("expected four rows left in orders_outbox, got %d", outboxTableRowCount(t, store, "orders_outbox"))
    }

    if 1 != outboxTableRowCount(t, store, DefaultTableName) {
        t.Fatalf("expected melody_outbox untouched, got %d rows", outboxTableRowCount(t, store, DefaultTableName))
    }
}

func TestStore_AMalformedTableNameIsRefused(t *testing.T) {
    database := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN("postgres://nobody@127.0.0.1:1/none?sslmode=disable"))), pgdialect.New())
    defer database.Close()

    if DefaultTableName != NewStore(database, &stringCodec{}, StoreConfig{}).TableName() {
        t.Fatalf("expected an empty table name to take the default")
    }

    if strings.Repeat("a", 64) != NewStore(database, &stringCodec{}, StoreConfig{TableName: strings.Repeat("a", 64)}).TableName() {
        t.Fatalf("expected a 64-character identifier admitted")
    }

    for _, tableName := range []string{"a.b", "a b", "a`", "1orders", "orders-outbox", strings.Repeat("a", 65)} {
        expectPanicNamingField(t, "StoreConfig.TableName", func() {
            NewStore(database, &stringCodec{}, StoreConfig{TableName: tableName})
        })
    }

    expectPanicNamingField(t, "StoreConfig.Headers.MaxCount", func() {
        NewStore(database, &stringCodec{}, StoreConfig{Headers: messagebus.HeaderLimitsConfig{MaxCount: bound.UnlimitedCount()}})
    })
}

func TestStore_EnqueueStoresHeadersAndTransportAndTheClaimReturnsThem(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()
    runtimeInstance := outboxTestRuntime(ctx)

    if enqueueErr := store.Enqueue(ctx, store.database, "plain", EnqueueOptions{}); nil != enqueueErr {
        t.Fatalf("enqueue: %v", enqueueErr)
    }

    if enqueueErr := store.Enqueue(ctx, store.database, "named", EnqueueOptions{Headers: map[string]string{"request-id": "request-7"}, Transport: "orders"}); nil != enqueueErr {
        t.Fatalf("enqueue: %v", enqueueErr)
    }

    nullColumns := 0
    if scanErr := store.database.NewRaw("SELECT count(*) FROM melody_outbox WHERE headers IS NULL AND transport IS NULL").Scan(ctx, &nullColumns); nil != scanErr || 1 != nullColumns {
        t.Fatalf("expected NULL for both columns left out, got %d: %v", nullColumns, scanErr)
    }

    plain, plainErr := store.ClaimDueMessages(runtimeInstance, 10, time.Hour, ClaimOptions{})
    if nil != plainErr || 1 != len(plain) || "" != plain[0].Headers || "" != plain[0].Transport {
        t.Fatalf("expected the plain row with neither, got %+v: %v", plain, plainErr)
    }

    named, namedErr := store.ClaimDueMessages(runtimeInstance, 10, time.Hour, ClaimOptions{Transport: "orders"})
    if nil != namedErr || 1 != len(named) || `{"request-id":"request-7"}` != named[0].Headers || "orders" != named[0].Transport {
        t.Fatalf("expected the named row with its headers and transport, got %+v: %v", named, namedErr)
    }
}

func TestStore_AClaimSeesOnlyItsTransport(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()
    runtimeInstance := outboxTestRuntime(ctx)

    for _, transportName := range []string{"", "a", "b", "", "a", "b"} {
        if enqueueErr := store.Enqueue(ctx, store.database, transportName, EnqueueOptions{Transport: transportName}); nil != enqueueErr {
            t.Fatalf("enqueue: %v", enqueueErr)
        }
    }

    seen := map[int64]bool{}
    for _, transportName := range []string{"", "a", "b"} {
        claimed, claimErr := store.ClaimDueMessages(runtimeInstance, 10, time.Hour, ClaimOptions{Transport: transportName})
        if nil != claimErr || 2 != len(claimed) {
            t.Fatalf("claim %q: %v (got %d)", transportName, claimErr, len(claimed))
        }

        for _, pending := range claimed {
            if transportName != pending.Transport || true == seen[pending.Id] {
                t.Fatalf("claim %q answered a row of %q or a row twice", transportName, pending.Transport)
            }

            seen[pending.Id] = true
        }
    }
}

func TestStore_EnqueueAndClaimRefuseAMalformedTransportOrHeaders(t *testing.T) {
    store := outboxTestStoreWith(t, StoreConfig{Headers: messagebus.HeaderLimitsConfig{MaxCount: bound.Of(1)}})
    ctx := context.Background()

    expectErrorNamingField(t, store.Enqueue(ctx, store.database, "x", EnqueueOptions{Transport: "-orders"}), "EnqueueOptions.Transport")

    headersErr := store.Enqueue(ctx, store.database, "x", EnqueueOptions{Headers: map[string]string{"a": "1", "b": "2"}})
    if false == errors.Is(headersErr, messagebus.ErrHeadersRefused) {
        t.Fatalf("expected the enqueue refused by ErrHeadersRefused, got %v", headersErr)
    }
    expectErrorNamingField(t, headersErr, "StoreConfig.Headers.MaxCount")

    _, claimErr := store.ClaimDueMessages(outboxTestRuntime(ctx), 1, time.Hour, ClaimOptions{Transport: "a b"})
    expectErrorNamingField(t, claimErr, "ClaimOptions.Transport")

    if 0 != outboxTableRowCount(t, store, DefaultTableName) {
        t.Fatalf("expected no row written by a refused enqueue")
    }
}

func TestStore_EnsureSchemaAddsTheTwoColumnsToAnOlderTable(t *testing.T) {
    store := outboxTestStore(t)
    ctx := context.Background()

    if _, dropErr := store.database.NewRaw("ALTER TABLE melody_outbox DROP COLUMN headers, DROP COLUMN transport").Exec(ctx); nil != dropErr {
        t.Fatalf("drop the columns: %v", dropErr)
    }

    for range 2 {
        if schemaErr := store.EnsureSchema(outboxTestRuntime(ctx)); nil != schemaErr {
            t.Fatalf("ensure schema: %v", schemaErr)
        }
    }

    columns := 0
    if scanErr := store.database.NewRaw("SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'melody_outbox' AND column_name IN ('headers', 'transport') AND is_nullable = 'YES'").Scan(ctx, &columns); nil != scanErr || 2 != columns {
        t.Fatalf("expected the two nullable columns added once, got %d: %v", columns, scanErr)
    }

    if nilErr := store.EnsureSchema(nil); nil == nilErr || false == strings.Contains(nilErr.Error(), "the runtime is nil or carries no context") {
        t.Fatalf("expected a nil runtime refused, got %v", nilErr)
    }
}
