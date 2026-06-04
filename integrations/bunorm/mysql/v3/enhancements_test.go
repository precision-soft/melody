package mysql_test

import (
    "context"
    "database/sql"
    "os"
    "strings"
    "testing"

    _ "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
)

type secretRecord struct {
    bun.BaseModel `bun:"table:secret_record"`

    Id     int64                   `bun:"id,pk,autoincrement"`
    Secret encrypt.EncryptedString `bun:"secret,notnull,type:varchar(255)"`
}

type widget struct {
    Id       int64  `bun:"id,pk"`
    Name     string `bun:"name"`
    Quantity int    `bun:"quantity"`
}

func newKey() []byte {
    key := make([]byte, 32)
    for index := range key {
        key[index] = byte(index + 1)
    }
    return key
}

func TestBunormEncryption_CiphertextAtRest(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping bunorm encryption integration test")
    }

    ctx := context.Background()

    sqlDb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqlDb.Close()

    database := bun.NewDB(sqlDb, mysqldialect.New())

    database.ExecContext(ctx, "DROP TABLE IF EXISTS secret_record")
    if _, createErr := database.NewCreateTable().Model((*secretRecord)(nil)).Exec(ctx); nil != createErr {
        t.Fatalf("create secret_record: %v", createErr)
    }

    encrypt.UseCipher(encrypt.NewCipher(encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey()})))
    defer encrypt.UseCipher(nil)

    record := &secretRecord{Secret: "classified-data"}
    if _, insertErr := database.NewInsert().Model(record).Exec(ctx); nil != insertErr {
        t.Fatalf("insert: %v", insertErr)
    }

    loaded := new(secretRecord)
    if scanErr := database.NewSelect().Model(loaded).Where("id = ?", record.Id).Scan(ctx); nil != scanErr {
        t.Fatalf("select: %v", scanErr)
    }
    if "classified-data" != string(loaded.Secret) {
        t.Fatalf("expected decrypted value, got %q", loaded.Secret)
    }

    var rawSecret string
    if rawErr := sqlDb.QueryRowContext(ctx, "SELECT secret FROM secret_record WHERE id = ?", record.Id).Scan(&rawSecret); nil != rawErr {
        t.Fatalf("raw select: %v", rawErr)
    }
    if "classified-data" == rawSecret || false == strings.HasPrefix(rawSecret, "v1:") {
        t.Fatalf("expected ciphertext at rest, got %q", rawSecret)
    }
}

func TestBunormAudit_RecordsFieldLevelChangeSet(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping bunorm audit integration test")
    }

    ctx := context.Background()

    sqlDb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqlDb.Close()

    auditDatabase := bun.NewDB(sqlDb, mysqldialect.New())

    auditDatabase.ExecContext(ctx, "DROP TABLE IF EXISTS melody_audit")
    if _, createErr := auditDatabase.NewCreateTable().Model((*audit.Entry)(nil)).Exec(ctx); nil != createErr {
        t.Fatalf("create melody_audit: %v", createErr)
    }

    recorder := audit.NewRecorder(auditDatabase, "melody_audit")
    actorCtx := audit.WithActor(ctx, "alice")

    before := widget{Id: 1, Name: "bolt", Quantity: 5}
    after := widget{Id: 1, Name: "bolt", Quantity: 9}

    if recordErr := recorder.RecordUpdate(actorCtx, "widget", "1", before, after); nil != recordErr {
        t.Fatalf("record update: %v", recordErr)
    }

    var operation string
    var actor string
    var changes string
    selectErr := sqlDb.QueryRowContext(ctx, "SELECT operation, actor, changes FROM melody_audit ORDER BY id DESC LIMIT 1").Scan(&operation, &actor, &changes)
    if nil != selectErr {
        t.Fatalf("audit select: %v", selectErr)
    }

    if "UPDATE" != operation {
        t.Fatalf("expected UPDATE operation, got %q", operation)
    }

    if "alice" != actor {
        t.Fatalf("expected actor alice, got %q", actor)
    }

    if false == strings.Contains(changes, "\"quantity\"") {
        t.Fatalf("expected quantity in change-set, got %q", changes)
    }

    if false == strings.Contains(changes, "\"old\":5") || false == strings.Contains(changes, "\"new\":9") {
        t.Fatalf("expected old/new values in change-set, got %q", changes)
    }
}
