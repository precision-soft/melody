package config

import (
    "context"
    "database/sql"
    "os"
    "testing"
    "time"

    outbox "github.com/precision-soft/melody/integrations/outbox/v4"
    _ "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/v4/.example/message"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

func outboxSchemaTestRuntime(ctx context.Context) melodycontainercontract.Runtime {
    return melodyruntime.NewRun(ctx, melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
}

func outboxSchemaTestDatabase(t *testing.T) *bun.DB {
    t.Helper()

    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping the outbox schema MySQL twin")
    }

    sqlDatabase, openErr := sql.Open("mysql", dsn+"?parseTime=true")
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    t.Cleanup(func() { _ = sqlDatabase.Close() })

    database := bun.NewDB(sqlDatabase, mysqldialect.New())
    t.Cleanup(func() { _, _ = database.NewRaw("DROP TABLE IF EXISTS orders_outbox").Exec(context.Background()) })

    return database
}

func TestOutboxSchema_EnsureSchemaAddsTheSendTimeOnMySql(t *testing.T) {
    database := outboxSchemaTestDatabase(t)
    store := outbox.NewStore(database, &outboxNoticeCodec{}, outbox.StoreConfig{})
    ctx := context.Background()

    if schemaErr := store.EnsureSchema(outboxSchemaTestRuntime(ctx)); nil != schemaErr {
        t.Fatalf("ensure schema: %v", schemaErr)
    }

    if _, dropErr := database.NewRaw("ALTER TABLE melody_outbox DROP COLUMN sent_at").Exec(ctx); nil != dropErr {
        t.Fatalf("drop the column: %v", dropErr)
    }

    for range 2 {
        if schemaErr := store.EnsureSchema(outboxSchemaTestRuntime(ctx)); nil != schemaErr {
            t.Fatalf("ensure schema: %v", schemaErr)
        }
    }

    columnType := ""
    if scanErr := database.NewRaw("SELECT column_type FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'melody_outbox' AND column_name = 'sent_at'").Scan(ctx, &columnType); nil != scanErr || "datetime" != columnType {
        t.Fatalf("expected the column added once as datetime, got %q: %v", columnType, scanErr)
    }
}

func TestOutboxSchema_EnsureSchemaAddsTheHeadersAndTheTransportOnMySql(t *testing.T) {
    database := outboxSchemaTestDatabase(t)
    store := outbox.NewStore(database, &outboxNoticeCodec{}, outbox.StoreConfig{})
    ctx := context.Background()

    if schemaErr := store.EnsureSchema(outboxSchemaTestRuntime(ctx)); nil != schemaErr {
        t.Fatalf("ensure schema: %v", schemaErr)
    }

    if _, dropErr := database.NewRaw("ALTER TABLE melody_outbox DROP COLUMN headers, DROP COLUMN transport").Exec(ctx); nil != dropErr {
        t.Fatalf("drop the columns: %v", dropErr)
    }

    if _, dropErr := database.NewRaw("DROP TABLE IF EXISTS orders_outbox").Exec(ctx); nil != dropErr {
        t.Fatalf("drop the fresh table: %v", dropErr)
    }

    fresh := outbox.NewStore(database, &outboxNoticeCodec{}, outbox.StoreConfig{TableName: "orders_outbox"})

    for _, schemaStore := range []*outbox.Store{store, store, fresh} {
        if schemaErr := schemaStore.EnsureSchema(outboxSchemaTestRuntime(ctx)); nil != schemaErr {
            t.Fatalf("ensure schema: %v", schemaErr)
        }
    }

    for _, tableName := range []string{"melody_outbox", "orders_outbox"} {
        columns := 0
        if scanErr := database.NewRaw("SELECT count(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name IN ('headers', 'transport') AND is_nullable = 'YES'", tableName).Scan(ctx, &columns); nil != scanErr || 2 != columns {
            t.Fatalf("%s: expected the two nullable columns, got %d: %v", tableName, columns, scanErr)
        }
    }
}

func TestOutboxSchema_AConfiguredTableServesTheClaimTheResolutionsAndTheRequeueOnMySql(t *testing.T) {
    database := outboxSchemaTestDatabase(t)
    ctx := context.Background()
    runtimeInstance := outboxSchemaTestRuntime(ctx)

    if _, dropErr := database.NewRaw("DROP TABLE IF EXISTS orders_outbox").Exec(ctx); nil != dropErr {
        t.Fatalf("drop: %v", dropErr)
    }

    store := outbox.NewStore(database, &outboxNoticeCodec{}, outbox.StoreConfig{TableName: "orders_outbox"})
    if schemaErr := store.EnsureSchema(runtimeInstance); nil != schemaErr {
        t.Fatalf("ensure schema: %v", schemaErr)
    }

    for _, reference := range []string{"sent", "dead"} {
        if enqueueErr := store.Enqueue(ctx, database, message.OutboxNotice{Reference: reference}, outbox.EnqueueOptions{Headers: map[string]string{"request-id": reference}, Transport: "notices"}); nil != enqueueErr {
            t.Fatalf("enqueue: %v", enqueueErr)
        }
    }

    /* a DATETIME column rounds the enqueue's fractional second, up as often as not, so the rows are due a second later */
    time.Sleep(1100 * time.Millisecond)

    claimed, claimErr := store.ClaimDueMessages(runtimeInstance, 10, time.Hour, outbox.ClaimOptions{Transport: "notices"})
    if nil != claimErr || 2 != len(claimed) || `{"request-id":"sent"}` != claimed[0].Headers {
        t.Fatalf("claim: %v (got %+v)", claimErr, claimed)
    }

    if markErr := store.MarkSent(runtimeInstance, claimed[0].Id, claimed[0].ClaimToken); nil != markErr {
        t.Fatalf("mark sent: %v", markErr)
    }

    if deadErr := store.MarkDead(runtimeInstance, claimed[1].Id, 1, "poison", claimed[1].ClaimToken); nil != deadErr {
        t.Fatalf("mark dead: %v", deadErr)
    }

    if requeued, requeueErr := store.RequeueDead(runtimeInstance, outbox.RequeueDeadOptions{}); nil != requeueErr || 1 != requeued {
        t.Fatalf("requeue dead: %v (requeued %d)", requeueErr, requeued)
    }

    if pruned, pruneErr := store.PruneSent(runtimeInstance, outbox.PruneSentOptions{SentBefore: time.Now().Add(time.Hour)}); nil != pruneErr || 1 != pruned {
        t.Fatalf("prune: %v (pruned %d)", pruneErr, pruned)
    }
}
