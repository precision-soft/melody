package config

import (
    "context"
    "database/sql"
    "os"
    "testing"

    outbox "github.com/precision-soft/melody/integrations/outbox/v4"
    _ "github.com/go-sql-driver/mysql"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

/* the MySQL arm of the outbox schema runs here, where the example keeps its outbox: a table created without sent_at gains it once, as DATETIME like every other time column bun gives it */
func TestOutboxSchema_EnsureSchemaAddsTheSendTimeOnMySql(t *testing.T) {
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
    store := outbox.NewStore(database, &outboxNoticeCodec{})
    ctx := context.Background()

    if schemaErr := store.EnsureSchema(ctx); nil != schemaErr {
        t.Fatalf("ensure schema: %v", schemaErr)
    }

    if _, dropErr := database.NewRaw("ALTER TABLE melody_outbox DROP COLUMN sent_at").Exec(ctx); nil != dropErr {
        t.Fatalf("drop the column: %v", dropErr)
    }

    for range 2 {
        if schemaErr := store.EnsureSchema(ctx); nil != schemaErr {
            t.Fatalf("ensure schema: %v", schemaErr)
        }
    }

    columnType := ""
    if scanErr := database.NewRaw("SELECT column_type FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'melody_outbox' AND column_name = 'sent_at'").Scan(ctx, &columnType); nil != scanErr || "datetime" != columnType {
        t.Fatalf("expected the column added once as datetime, got %q: %v", columnType, scanErr)
    }
}
