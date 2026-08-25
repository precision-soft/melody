package mysql

import (
    "bytes"
    "context"
    "database/sql"
    "os"
    "strconv"
    "strings"
    "testing"

    _ "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    "github.com/precision-soft/melody/integrations/bunorm/v3/audit"
    "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

type secretRecord struct {
    bun.BaseModel `bun:"table:secret_record"`

    Id     int64                   `bun:"id,pk,autoincrement"`
    Secret encrypt.EncryptedString `bun:"secret,notnull,type:varchar(255)"`
}

type lookupRecord struct {
    bun.BaseModel `bun:"table:lookup_record"`

    Id    int64                                `bun:"id,pk,autoincrement"`
    Email encrypt.EncryptedDeterministicString `bun:"email,notnull,type:varbinary(255)"`
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
    if "classified-data" == rawSecret {
        t.Fatalf("expected ciphertext at rest, got plaintext")
    }
    if false == strings.HasPrefix(rawSecret, "<ENC>\x00gcm1\x00") {
        t.Fatalf("expected the encryption marker (with nul glue bytes intact) at rest, got %q", rawSecret)
    }
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

    cipher := encrypt.NewCipher(encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey()}))
    encrypt.UseCipher(cipher)
    defer encrypt.UseCipher(nil)

    for _, email := range []string{"alice@example.com", "bob@example.com", "alice@example.com"} {
        record := &lookupRecord{Email: encrypt.EncryptedDeterministicString(email)}
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

/* normalisingCipher is an application that writes the row while the bulk run holds the value it read: the value is normalised between the run's SELECT and its guarded UPDATE, which is the most likely write there is to race a bulk encryption. */
type normalisingCipher struct {
    encrypt.Cipher

    ctx         context.Context
    database    *bun.DB
    table       string
    column      string
    normalising string
    stored      string
    updateErr   error
}

/* the seam fires only for the ROW's value: MigrateEncrypt now runs its own capacity check first, whose width probe seals filler strings through this same Encrypt — an unconditional trigger normalised the row BEFORE the run's SELECT, the run then read the already-normalised value, and the guard had no race left to catch. The race this test builds is the one between the run's SELECT and its guarded UPDATE, and only the row's own encrypt call sits in that window. */
func (instance *normalisingCipher) Encrypt(plaintext string) (string, error) {
    if plaintext == instance.stored {
        _, execErr := instance.database.ExecContext(
            instance.ctx,
            "UPDATE "+instance.table+" SET "+instance.column+" = "+instance.normalising,
        )
        if nil != execErr {
            instance.updateErr = execErr
        }
    }

    return instance.Cipher.Encrypt(plaintext)
}

/* the bulk run guards its UPDATE on the value it read, and a bare `column = ?` is evaluated under the COLUMN's collation. Under MySQL 8's default utf8mb4_0900_ai_ci that equates 'Bob@Example.com' with the 'bob@example.com' a concurrent normalisation has just written, so the update applies, the application's write is destroyed, the stored ciphertext decrypts to casing that was already replaced, and neither a skip nor a non-zero exit is reported. */
func TestEncryptMigrate_ConcurrentNormalisationIsSkippedUnderTheDefaultCollation(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping bunorm encrypt migrate integration test")
    }

    ctx := context.Background()

    sqlDb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqlDb.Close()

    database := bun.NewDB(sqlDb, mysqldialect.New())

    table := "migrate_collation_record"

    cases := []struct {
        name        string
        collation   string
        stored      string
        normalising string
        normalised  string
    }{
        /* the 8.x default is accent- and case-insensitive, so a casing normalisation still matches the value that was read */
        {
            name:        "utf8mb4_0900_ai_ci case normalisation",
            collation:   "utf8mb4_0900_ai_ci",
            stored:      "Bob@Example.com",
            normalising: "LOWER(email)",
            normalised:  "bob@example.com",
        },
        /* a PAD SPACE collation ignores trailing whitespace, so trimming it matches too */
        {
            name:        "utf8mb4_general_ci whitespace trim",
            collation:   "utf8mb4_general_ci",
            stored:      "bob@example.com  ",
            normalising: "TRIM(TRAILING ' ' FROM email)",
            normalised:  "bob@example.com",
        },
    }

    for _, testCase := range cases {
        database.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)

        createSql := "CREATE TABLE " + table + " (" +
            "id BIGINT NOT NULL PRIMARY KEY, " +
            "email VARCHAR(255) CHARACTER SET utf8mb4 COLLATE " + testCase.collation + " NOT NULL" +
            ") CHARACTER SET utf8mb4 COLLATE " + testCase.collation
        if _, createErr := database.ExecContext(ctx, createSql); nil != createErr {
            t.Fatalf("%s: create %s: %v", testCase.name, table, createErr)
        }

        if _, insertErr := database.ExecContext(ctx, "INSERT INTO "+table+" (id, email) VALUES (1, ?)", testCase.stored); nil != insertErr {
            t.Fatalf("%s: insert: %v", testCase.name, insertErr)
        }

        cipher := &normalisingCipher{
            Cipher:      encrypt.NewCipher(encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey()})),
            ctx:         ctx,
            database:    database,
            table:       table,
            column:      "email",
            normalising: testCase.normalising,
            stored:      testCase.stored,
        }

        /* the normalisation writes the value the run is about to overwrite; the guard must notice and leave the row alone */
        if _, updateErr := database.ExecContext(ctx, "UPDATE "+table+" SET email = ? WHERE id = 1", testCase.stored); nil != updateErr {
            t.Fatalf("%s: reset: %v", testCase.name, updateErr)
        }

        migrator := encrypt.NewMigrator(database, cipher)

        processed, runErr := migrator.MigrateEncrypt(ctx, encrypt.TableSpec{
            Table:      table,
            PrimaryKey: "id",
            Columns:    []string{"email"},
        })

        if nil != cipher.updateErr {
            t.Fatalf("%s: concurrent normalisation: %v", testCase.name, cipher.updateErr)
        }

        var stored string
        if scanErr := sqlDb.QueryRowContext(ctx, "SELECT email FROM "+table+" WHERE id = 1").Scan(&stored); nil != scanErr {
            t.Fatalf("%s: raw select: %v", testCase.name, scanErr)
        }

        if nil == runErr {
            t.Fatalf(
                "%s: expected the row that changed under the run to be skipped and reported; the run reported processed=%d with no error and stored %q",
                testCase.name,
                processed,
                stored,
            )
        }

        if false == strings.Contains(runErr.Error(), "untouched") {
            t.Fatalf("%s: expected the skip to be reported, got %v", testCase.name, runErr)
        }

        if 0 != processed {
            t.Fatalf("%s: expected the skipped row not to be counted as processed, got %d", testCase.name, processed)
        }

        if testCase.normalised != stored {
            t.Fatalf("%s: expected the concurrent normalisation to survive as %q, got %q", testCase.name, testCase.normalised, stored)
        }
    }

    database.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)
}

/* sealing expands a value by roughly a third plus fifty characters, so an in-place encryption of a VARCHAR(255) overflows from 153 bytes of plaintext. MySQL's default strict sql_mode turns that into a clean failure; on a server left non-strict the overflowing UPDATE succeeds with a warning, the row is counted as migrated, the command exits zero, and the truncated ciphertext will never authenticate again — the plaintext is simply gone. */
func TestEncryptMigrate_RefusesATargetColumnTooNarrowForTheCiphertext(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping bunorm encrypt migrate integration test")
    }

    ctx := context.Background()

    sqlDb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqlDb.Close()

    /* the non-strict sql_mode below is a session setting, so the run has to land on the same connection */
    sqlDb.SetMaxOpenConns(1)

    database := bun.NewDB(sqlDb, mysqldialect.New())

    table := "migrate_width_record"

    database.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)
    defer database.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)

    createSql := "CREATE TABLE " + table + " (" +
        "id BIGINT NOT NULL PRIMARY KEY, " +
        "secret VARCHAR(255) NOT NULL" +
        ")"
    if _, createErr := database.ExecContext(ctx, createSql); nil != createErr {
        t.Fatalf("create %s: %v", table, createErr)
    }

    plaintext := strings.Repeat("s", 200)
    if _, insertErr := database.ExecContext(ctx, "INSERT INTO "+table+" (id, secret) VALUES (1, ?)", plaintext); nil != insertErr {
        t.Fatalf("insert: %v", insertErr)
    }

    if _, modeErr := database.ExecContext(ctx, "SET SESSION sql_mode = ''"); nil != modeErr {
        t.Fatalf("relax sql_mode: %v", modeErr)
    }
    defer database.ExecContext(ctx, "SET SESSION sql_mode = DEFAULT")

    cipher := encrypt.NewCipher(encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey()}))
    migrator := encrypt.NewMigrator(database, cipher)

    spec := encrypt.TableSpec{Table: table, PrimaryKey: "id", Columns: []string{"secret"}}

    capacityErr := migrator.EnsureColumnCapacity(ctx, spec)
    if nil == capacityErr {
        processed, runErr := migrator.MigrateEncrypt(ctx, spec)

        var truncated string
        sqlDb.QueryRowContext(ctx, "SELECT secret FROM "+table+" WHERE id = 1").Scan(&truncated)

        decrypted, decryptErr := cipher.Decrypt(truncated)

        t.Fatalf(
            "expected a 255 character column to be refused for a %d byte plaintext; the run reported processed=%d err=%v, left %d characters at rest, and reading them back (err=%v) yields %d characters, plaintext recovered=%t",
            len(plaintext),
            processed,
            runErr,
            len(truncated),
            decryptErr,
            len(decrypted),
            plaintext == decrypted,
        )
    }

    contextProvider, carriesContext := capacityErr.(exceptioncontract.ContextProvider)
    if false == carriesContext {
        t.Fatalf("expected the refusal to carry a diagnostic context, got %T", capacityErr)
    }

    diagnostic := contextProvider.Context()
    if table != diagnostic["table"] || "secret" != diagnostic["column"] {
        t.Fatalf("expected the refusal to name the table and the column, got %v", diagnostic)
    }

    /* 11 marker characters, a two character key id, a colon and the base64 of a 12 byte nonce, 200 plaintext bytes and a 16 byte tag */
    if 255 != diagnostic["width"] || 318 != diagnostic["requiredWidth"] {
        t.Fatalf("expected the refusal to report width 255 and required width 318, got %v", diagnostic)
    }

    var untouched string
    if scanErr := sqlDb.QueryRowContext(ctx, "SELECT secret FROM "+table+" WHERE id = 1").Scan(&untouched); nil != scanErr {
        t.Fatalf("raw select: %v", scanErr)
    }
    if plaintext != untouched {
        t.Fatalf("expected the refused run to leave the row untouched, got %d characters", len(untouched))
    }

    /* the width the refusal named must be the width that lets the run through */
    if _, widenErr := database.ExecContext(ctx, "ALTER TABLE "+table+" MODIFY secret VARCHAR(318) NOT NULL"); nil != widenErr {
        t.Fatalf("widen column: %v", widenErr)
    }

    if recheckErr := migrator.EnsureColumnCapacity(ctx, spec); nil != recheckErr {
        t.Fatalf("expected the widened column to be accepted, got %v", recheckErr)
    }

    processed, runErr := migrator.MigrateEncrypt(ctx, spec)
    if nil != runErr {
        t.Fatalf("migrate the widened column: %v", runErr)
    }
    if 1 != processed {
        t.Fatalf("expected one migrated row, got %d", processed)
    }

    var sealed string
    if scanErr := sqlDb.QueryRowContext(ctx, "SELECT secret FROM "+table+" WHERE id = 1").Scan(&sealed); nil != scanErr {
        t.Fatalf("raw select: %v", scanErr)
    }

    decrypted, decryptErr := cipher.Decrypt(sealed)
    if nil != decryptErr {
        t.Fatalf("decrypt the migrated value: %v", decryptErr)
    }
    if plaintext != decrypted {
        t.Fatalf("expected the migrated value to decrypt to the original plaintext, got %d characters", len(decrypted))
    }

    /* a second run over the already-sealed column must not read the ciphertext as a plaintext to be sealed again and demand a wider column for it */
    if rerunErr := migrator.EnsureColumnCapacity(ctx, spec); nil != rerunErr {
        t.Fatalf("expected a re-run over an already-migrated column to be accepted, got %v", rerunErr)
    }
}

/* the key id is stored inside every sealed value, so rotating onto a longer one grows the whole column by the difference at once — thirteen characters here. A column sized exactly to the ciphertext it holds is then too narrow for all of it, and on a non-strict server that is every row truncated to a ciphertext that can never authenticate again while the run reports success. */
func TestEncryptMigrate_RefusesARotationToALongerKeyId(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping bunorm encrypt rotate integration test")
    }

    ctx := context.Background()

    sqlDb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqlDb.Close()

    /* the non-strict sql_mode below is a session setting, so the run has to land on the same connection */
    sqlDb.SetMaxOpenConns(1)

    database := bun.NewDB(sqlDb, mysqldialect.New())

    table := "migrate_rotate_width_record"

    database.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)
    defer database.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)

    rotationKey := make([]byte, 32)
    for index := range rotationKey {
        rotationKey[index] = byte(255 - index)
    }

    targetKeyId := "2026-07-rotated"

    cipher := encrypt.NewCipher(encrypt.NewStaticKeyProvider(
        "v1",
        map[string][]byte{"v1": newKey(), targetKeyId: rotationKey},
    ))

    plaintext := strings.Repeat("s", 100)

    sealed, sealErr := cipher.EncryptWithKeyId(plaintext, "v1")
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    /* the column is sized to exactly what it already holds, which is what a schema written for the current key looks like */
    createSql := "CREATE TABLE " + table + " (" +
        "id BIGINT NOT NULL PRIMARY KEY, " +
        "secret VARCHAR(" + strconv.Itoa(len(sealed)) + ") NOT NULL" +
        ")"
    if _, createErr := database.ExecContext(ctx, createSql); nil != createErr {
        t.Fatalf("create %s: %v", table, createErr)
    }

    /* the driver handle rather than bun: bun renders a string argument into the statement itself and drops the nul bytes the marker is glued with, which would store a value that no longer reads as sealed at all */
    if _, insertErr := sqlDb.ExecContext(ctx, "INSERT INTO "+table+" (id, secret) VALUES (1, ?)", sealed); nil != insertErr {
        t.Fatalf("insert the sealed row: %v", insertErr)
    }

    /* a column mid-migration still holds plaintext, and a rotation seals that under the target key too */
    shortPlaintext := strings.Repeat("p", 10)
    if _, insertErr := sqlDb.ExecContext(ctx, "INSERT INTO "+table+" (id, secret) VALUES (2, ?)", shortPlaintext); nil != insertErr {
        t.Fatalf("insert the plaintext row: %v", insertErr)
    }

    if _, modeErr := database.ExecContext(ctx, "SET SESSION sql_mode = ''"); nil != modeErr {
        t.Fatalf("relax sql_mode: %v", modeErr)
    }
    defer database.ExecContext(ctx, "SET SESSION sql_mode = DEFAULT")

    migrator := encrypt.NewMigrator(database, cipher)

    spec := encrypt.TableSpec{Table: table, PrimaryKey: "id", Columns: []string{"secret"}}

    capacityErr := migrator.EnsureColumnCapacityForReencrypt(ctx, spec, targetKeyId)
    if nil == capacityErr {
        processed, runErr := migrator.MigrateReencrypt(ctx, spec, targetKeyId)

        var truncated string
        sqlDb.QueryRowContext(ctx, "SELECT secret FROM "+table+" WHERE id = 1").Scan(&truncated)

        decrypted, decryptErr := cipher.Decrypt(truncated)

        t.Fatalf(
            "expected a %d character column to be refused for a rotation from %q to %q; the run reported processed=%d err=%v, left %d characters at rest, and reading them back (err=%v) yields %d characters, plaintext recovered=%t",
            len(sealed),
            "v1",
            targetKeyId,
            processed,
            runErr,
            len(truncated),
            decryptErr,
            len(decrypted),
            plaintext == decrypted,
        )
    }

    contextProvider, carriesContext := capacityErr.(exceptioncontract.ContextProvider)
    if false == carriesContext {
        t.Fatalf("expected the refusal to carry a diagnostic context, got %T", capacityErr)
    }

    diagnostic := contextProvider.Context()
    if table != diagnostic["table"] || "secret" != diagnostic["column"] {
        t.Fatalf("expected the refusal to name the table and the column, got %v", diagnostic)
    }

    /* every stored value grows by exactly the difference between the two key ids */
    requiredWidth := len(sealed) + len(targetKeyId) - len("v1")
    if len(sealed) != diagnostic["width"] || requiredWidth != diagnostic["requiredWidth"] {
        t.Fatalf("expected the refusal to report width %d and required width %d, got %v", len(sealed), requiredWidth, diagnostic)
    }

    if targetKeyId != diagnostic["targetKeyId"] {
        t.Fatalf("expected the refusal to name the key id it was asked to rotate to, got %v", diagnostic)
    }

    var untouched string
    if scanErr := sqlDb.QueryRowContext(ctx, "SELECT secret FROM "+table+" WHERE id = 1").Scan(&untouched); nil != scanErr {
        t.Fatalf("raw select: %v", scanErr)
    }
    if sealed != untouched {
        t.Fatalf("expected the refused rotation to leave the ciphertext untouched, got %d characters", len(untouched))
    }

    var untouchedPlaintext string
    if scanErr := sqlDb.QueryRowContext(ctx, "SELECT secret FROM "+table+" WHERE id = 2").Scan(&untouchedPlaintext); nil != scanErr {
        t.Fatalf("raw select: %v", scanErr)
    }
    if shortPlaintext != untouchedPlaintext {
        t.Fatalf("expected the refused rotation to leave the plaintext row untouched, got %q", untouchedPlaintext)
    }

    /* the width the refusal named must be the width that lets the rotation through */
    if _, widenErr := database.ExecContext(
        ctx,
        "ALTER TABLE "+table+" MODIFY secret VARCHAR("+strconv.Itoa(requiredWidth)+") NOT NULL",
    ); nil != widenErr {
        t.Fatalf("widen column: %v", widenErr)
    }

    if recheckErr := migrator.EnsureColumnCapacityForReencrypt(ctx, spec, targetKeyId); nil != recheckErr {
        t.Fatalf("expected the widened column to be accepted, got %v", recheckErr)
    }

    processed, runErr := migrator.MigrateReencrypt(ctx, spec, targetKeyId)
    if nil != runErr {
        t.Fatalf("rotate the widened column: %v", runErr)
    }
    if 2 != processed {
        t.Fatalf("expected two rotated rows, got %d", processed)
    }

    var rotated string
    if scanErr := sqlDb.QueryRowContext(ctx, "SELECT secret FROM "+table+" WHERE id = 1").Scan(&rotated); nil != scanErr {
        t.Fatalf("raw select: %v", scanErr)
    }

    if false == strings.Contains(rotated, targetKeyId) {
        t.Fatalf("expected the rotated value to carry the target key id, got %q", rotated)
    }

    decrypted, decryptErr := cipher.Decrypt(rotated)
    if nil != decryptErr {
        t.Fatalf("decrypt the rotated value: %v", decryptErr)
    }
    if plaintext != decrypted {
        t.Fatalf("expected the rotated value to decrypt to the original plaintext, got %d characters", len(decrypted))
    }

    /* a rotation that changes no key id lengthens nothing, so a column that already holds the result must not be refused */
    if rerunErr := migrator.EnsureColumnCapacityForReencrypt(ctx, spec, targetKeyId); nil != rerunErr {
        t.Fatalf("expected a re-run of the same rotation to be accepted, got %v", rerunErr)
    }
}
