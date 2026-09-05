package encrypt

import (
    "bytes"
    "context"
    "database/sql"
    "encoding/json"
    "os"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"

    _ "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

func TestEncryptedDeterministicString_MarshalJSONRedactsPlaintext(t *testing.T) {
    payload, marshalErr := json.Marshal(EncryptedDeterministicString("super-secret"))
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

func TestEncryptedDeterministicString_ValuePreservesMarkerBytes(t *testing.T) {
    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(7)})
    UseCipher(NewCipher(provider))
    defer UseCipher(nil)

    original := EncryptedDeterministicString("alice@example.com")

    stored, valueErr := original.Value()
    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    storedBytes, isBytes := stored.([]byte)
    if false == isBytes {
        t.Fatalf("deterministic value must be []byte so bun emits a binary literal, got %T", stored)
    }

    if false == strings.Contains(string(storedBytes), "\x00") {
        t.Fatalf("expected the encryption marker nul bytes to survive in the stored value")
    }

    var loaded EncryptedDeterministicString
    if scanErr := loaded.Scan(storedBytes); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    if "alice@example.com" != string(loaded) {
        t.Fatalf("round-trip mismatch: %q", loaded)
    }
}

type lookupRecord struct {
    bun.BaseModel `bun:"table:lookup_record"`

    Id    int64                                `bun:"id,pk,autoincrement"`
    Email EncryptedDeterministicString `bun:"email,notnull,type:varbinary(255)"`
}

func TestBunormDeterministicEncryption_SearchableAtRest(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping bunorm deterministic encryption integration test")
    }

    ctx := context.Background()

    sqlDb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqlDb.Close()

    database := bun.NewDB(sqlDb, mysqldialect.New())

    database.ExecContext(ctx, "DROP TABLE IF EXISTS lookup_record")
    if _, createErr := database.NewCreateTable().Model((*lookupRecord)(nil)).Exec(ctx); nil != createErr {
        t.Fatalf("create lookup_record: %v", createErr)
    }

    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newRampKey()}))
    UseCipher(cipher)
    defer UseCipher(nil)

    for _, email := range []string{"alice@example.com", "bob@example.com", "alice@example.com"} {
        record := &lookupRecord{Email: EncryptedDeterministicString(email)}
        if _, insertErr := database.NewInsert().Model(record).Exec(ctx); nil != insertErr {
            t.Fatalf("insert %q: %v", email, insertErr)
        }
    }

    var rawEmail []byte
    if rawErr := sqlDb.QueryRowContext(ctx, "SELECT email FROM lookup_record ORDER BY id LIMIT 1").Scan(&rawEmail); nil != rawErr {
        t.Fatalf("raw select: %v", rawErr)
    }
    if "alice@example.com" == string(rawEmail) {
        t.Fatalf("expected ciphertext at rest, got plaintext")
    }
    if false == bytes.Contains(rawEmail, []byte{0}) {
        t.Fatalf("expected the encryption marker nul bytes to survive at rest, got %q", rawEmail)
    }

    candidates, candidatesErr := cipher.CiphertextCandidates("alice@example.com")
    if nil != candidatesErr {
        t.Fatalf("candidates: %v", candidatesErr)
    }

    var matches []lookupRecord
    if scanErr := database.NewSelect().Model(&matches).Where("email IN (?)", bun.In(candidates)).Order("id").Scan(ctx); nil != scanErr {
        t.Fatalf("deterministic lookup: %v", scanErr)
    }

    if 2 != len(matches) {
        t.Fatalf("expected the deterministic IN lookup to find both alice rows, got %d", len(matches))
    }

    for _, match := range matches {
        if "alice@example.com" != string(match.Email) {
            t.Fatalf("expected a decrypted alice row, got %q", match.Email)
        }
    }
}

func TestEncryptedDeterministicString_UnmarshalJSONRefusesTheRedactionPlaceholder(t *testing.T) {
    payload, marshalErr := json.Marshal(EncryptedDeterministicString("lookup value"))
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    decoded := EncryptedDeterministicString("untouched")
    unmarshalErr := json.Unmarshal(payload, &decoded)
    if nil == unmarshalErr {
        t.Fatalf("expected the redacted document to be refused, got %q", string(decoded))
    }

    if "encrypt.EncryptedDeterministicString" != exception.LogContext(unmarshalErr)["type"] {
        t.Fatalf("expected the refusal to name the column type, got: %v", exception.LogContext(unmarshalErr))
    }

    if "untouched" != string(decoded) {
        t.Fatalf("expected the refused document to leave the value untouched, got %q", string(decoded))
    }
}

func TestEncryptedDeterministicString_UnmarshalJSONDecodesAPlaintextString(t *testing.T) {
    var decoded EncryptedDeterministicString
    if unmarshalErr := json.Unmarshal([]byte(`"hello"`), &decoded); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    if "hello" != string(decoded) {
        t.Fatalf("expected the plaintext to decode, got %q", string(decoded))
    }
}
