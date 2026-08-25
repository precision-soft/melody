package encrypt

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "os"
    "strings"
    "testing"

    _ "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

func TestEncryptedString_MarshalJSONRedactsPlaintext(t *testing.T) {
    payload, marshalErr := json.Marshal(EncryptedString("super-secret"))
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "super-secret") {
        t.Fatalf("plaintext leaked through json: %s", payload)
    }

    var decoded string
    if unmarshalErr := json.Unmarshal(payload, &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }
    if "<redacted>" != decoded {
        t.Fatalf("expected redacted json, got %q", decoded)
    }
}

func TestEncryptedString_MarshalJSONRedactsWhenNested(t *testing.T) {
    type holder struct {
        Email EncryptedString   `json:"email"`
        Tags  []EncryptedString `json:"tags"`
    }

    payload, marshalErr := json.Marshal(holder{Email: "ada@example.com", Tags: []EncryptedString{"tag-secret"}})
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    if true == strings.Contains(string(payload), "ada@example.com") || true == strings.Contains(string(payload), "tag-secret") {
        t.Fatalf("nested plaintext leaked through json: %s", payload)
    }
}

func TestEncryptedString_ValueScanRoundTrip(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(7)})
    UseCipher(NewCipher(provider))
    defer UseCipher(nil)

    original := EncryptedString("personal data")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    storedBytes, isBytes := stored.([]byte)
    if false == isBytes || "personal data" == string(storedBytes) {
        t.Fatalf("expected encrypted stored value, got %v", stored)
    }

    var loaded EncryptedString
    if scanErr := loaded.Scan(storedBytes); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "personal data" != string(loaded) {
        t.Fatalf("round-trip mismatch: %q", loaded)
    }
}

func TestEncryptedString_FailsClosedWithoutCipher(t *testing.T) {
    UseCipher(nil)

    secret := EncryptedString("personal data")
    if _, valueErr := secret.Value(); nil == valueErr {
        t.Fatalf("expected Value to fail when no cipher is configured")
    }

    var loaded EncryptedString
    if scanErr := loaded.Scan("anything"); nil == scanErr {
        t.Fatalf("expected Scan to fail when no cipher is configured")
    }

    if scanErr := loaded.Scan(nil); nil != scanErr {
        t.Fatalf("scan of NULL should succeed: %v", scanErr)
    }
}

func TestEncryptedString_MasksPlaintextWhenFormatted(t *testing.T) {
    secret := EncryptedString("personal data")

    if "personal data" == secret.String() {
        t.Fatalf("expected String to mask the plaintext")
    }

    for _, formatted := range []string{
        fmt.Sprintf("%v", secret),
        fmt.Sprintf("%s", secret),
        fmt.Sprintf("the value is %v", secret),
    } {
        if true == strings.Contains(formatted, "personal data") {
            t.Fatalf("expected formatted output to hide the plaintext, got %q", formatted)
        }
    }

    if "personal data" != string(secret) {
        t.Fatalf("explicit string conversion must still expose the value")
    }
}

/* the column read path is where a truncated ciphertext actually surfaces: Scan must refuse it rather than hand the model a marker and half a base64 blob as though the application had stored them. */
func TestEncryptedString_ScanReportsATruncatedCiphertext(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(7)})
    cipherInstance := NewCipher(provider)
    UseCipher(cipherInstance)
    defer UseCipher(nil)

    sealed, encryptErr := cipherInstance.Encrypt("personal data")
    if nil != encryptErr {
        t.Fatalf("encrypt: %v", encryptErr)
    }

    truncated := truncateSealed(t, sealed, 8)

    var scanned EncryptedString
    if scanErr := scanned.Scan([]byte(truncated)); nil == scanErr {
        t.Fatalf("expected scan to report the truncated ciphertext, got %q", string(scanned))
    }

    if "" != string(scanned) {
        t.Fatalf("expected no value on a failed scan, got %q", string(scanned))
    }
}

/* a column still holding unconverted rows must keep reading while it is encrypted one write at a time. */
func TestEncryptedString_ScanStillPassesGenuinePlaintextThrough(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(7)})
    UseCipher(NewCipher(provider))
    defer UseCipher(nil)

    var scanned EncryptedString
    if scanErr := scanned.Scan([]byte("not encrypted yet")); nil != scanErr {
        t.Fatalf("expected plaintext to pass through, got %v", scanErr)
    }

    if "not encrypted yet" != string(scanned) {
        t.Fatalf("expected the plaintext unchanged, got %q", string(scanned))
    }
}

type secretRecord struct {
    bun.BaseModel `bun:"table:secret_record"`

    Id     int64                   `bun:"id,pk,autoincrement"`
    Secret EncryptedString `bun:"secret,notnull,type:varchar(255)"`
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

    UseCipher(NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newRampKey()})))
    defer UseCipher(nil)

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
    if "classified-data" == rawSecret {
        t.Fatalf("expected ciphertext at rest, got plaintext")
    }
    if false == strings.HasPrefix(rawSecret, "<ENC>\x00gcm1\x00") {
        t.Fatalf("expected the encryption marker (with nul glue bytes intact) at rest, got %q", rawSecret)
    }
}
