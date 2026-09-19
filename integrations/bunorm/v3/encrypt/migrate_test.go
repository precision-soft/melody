package encrypt

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "io"
    "os"
    "strconv"
    "strings"
    "sync"
    "testing"

    _ "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

func TestEncryptTransform_DeterministicProducesSearchableCiphertext(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    deterministic, transformErr := migrator.encryptTransform(TableSpec{Deterministic: true})("alice@example.com")
    if nil != transformErr {
        t.Fatalf("deterministic encrypt transform: %v", transformErr)
    }
    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", deterministic) {
        t.Fatalf("expected the deterministic encrypt transform to be searchable via CiphertextCandidates")
    }

    randomized, randomizedErr := migrator.encryptTransform(TableSpec{Deterministic: false})("alice@example.com")
    if nil != randomizedErr {
        t.Fatalf("randomized encrypt transform: %v", randomizedErr)
    }
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", randomized) {
        t.Fatalf("expected the randomized encrypt transform to not match deterministic candidates")
    }
}

/* a column already bulk-encrypted with random nonces authenticates under a live key, so the deterministic seal used to pass every value through unchanged: the command reported success and rows processed while the column stayed randomized and every CiphertextCandidates equality lookup on it returned nothing. */
func TestEncryptTransform_DeterministicConvertsAnAlreadyRandomizedValue(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    randomized, randomizedErr := cipher.EncryptWithKeyId("alice@example.com", "v2")
    if nil != randomizedErr {
        t.Fatalf("randomized encrypt: %v", randomizedErr)
    }
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", randomized) {
        t.Fatalf("precondition: a randomized value must not be searchable")
    }

    converted, convertErr := migrator.encryptTransform(TableSpec{Deterministic: true})(randomized)
    if nil != convertErr {
        t.Fatalf("deterministic encrypt transform: %v", convertErr)
    }

    if converted == randomized {
        t.Fatalf("expected a deterministic encrypt to convert a randomized value rather than pass it through unchanged")
    }

    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", converted) {
        t.Fatalf("expected the converted value to be searchable via CiphertextCandidates")
    }
}

/* the conversion keeps the key the value already carries, so mode=encrypt never doubles as a key rotation; a retired key stays in the key set until re-encryption removes it, so the converted value still decrypts and still answers candidate lookups. */
func TestEncryptTransform_DeterministicKeepsTheKeyTheValueCarries(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    randomizedUnderRetiredKey, randomizedErr := cipher.EncryptWithKeyId("alice@example.com", "v1")
    if nil != randomizedErr {
        t.Fatalf("randomized encrypt: %v", randomizedErr)
    }

    converted, convertErr := migrator.encryptTransform(TableSpec{Deterministic: true})(randomizedUnderRetiredKey)
    if nil != convertErr {
        t.Fatalf("deterministic encrypt transform: %v", convertErr)
    }

    keyId, encrypted, _ := keyIdOf(converted)
    if false == encrypted || "v1" != keyId {
        t.Fatalf("expected the converted value to keep key v1, got %q (encrypted=%v)", keyId, encrypted)
    }

    if plaintext, _ := cipher.Decrypt(converted); "alice@example.com" != plaintext {
        t.Fatalf("expected the converted value to still decrypt to the original plaintext")
    }

    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", converted) {
        t.Fatalf("expected the converted value to be searchable via CiphertextCandidates")
    }
}

/* a second pass must be a no-op: a value already deterministic under its key transforms to itself, so applyRow emits no assignment and the row is honestly counted as processed. */
func TestEncryptTransform_DeterministicIsIdempotent(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    transform := migrator.encryptTransform(TableSpec{Deterministic: true})

    first, firstErr := transform("alice@example.com")
    if nil != firstErr {
        t.Fatalf("first deterministic encrypt transform: %v", firstErr)
    }

    second, secondErr := transform(first)
    if nil != secondErr {
        t.Fatalf("second deterministic encrypt transform: %v", secondErr)
    }

    if first != second {
        t.Fatalf("expected a deterministic encrypt to be idempotent, got %q then %q", first, second)
    }
}

/* inverted from the form that expected the value to be sealed as plaintext: the migrator reads STORED values, where a marker is the cipher's own claim of provenance, so a stored value that fails to decrypt is a missing key or a truncated write — sealing it destroyed the only remaining copy. The write-path leniency (an application string that merely looks like a marker) stays with the cipher's Encrypt; the bulk conversion stops and names the key instead. */
func TestEncryptTransform_StopsOnAStoredValueThatNoLongerDecrypts(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v2": newKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    foreign, foreignErr := NewCipher(NewStaticKeyProvider("v9", map[string][]byte{"v9": newKey(9)})).Encrypt("alice@example.com")
    if nil != foreignErr {
        t.Fatalf("foreign encrypt: %v", foreignErr)
    }

    for _, deterministic := range []bool{false, true} {
        _, convertErr := migrator.encryptTransform(TableSpec{Deterministic: deterministic})(foreign)
        if nil == convertErr {
            t.Fatalf("expected the stored undecryptable value to stop the run (deterministic=%v)", deterministic)
        }

        if false == strings.Contains(convertErr.Error(), "no longer decrypts") {
            t.Fatalf("expected the refusal to name the undecryptable value, got: %v", convertErr)
        }
    }
}

func TestReencryptTransform_ConvertsRandomizedSameKeyValueToDeterministic(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    randomizedUnderTarget, _ := cipher.EncryptWithKeyId("alice@example.com", "v2")
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", randomizedUnderTarget) {
        t.Fatalf("precondition: randomized value should not be searchable")
    }

    converted, convertErr := migrator.reencryptTransform(TableSpec{Deterministic: true}, "v2")(randomizedUnderTarget)
    if nil != convertErr {
        t.Fatalf("deterministic reencrypt transform: %v", convertErr)
    }
    if converted == randomizedUnderTarget {
        t.Fatalf("expected a deterministic reencrypt to convert a randomized same-key value rather than skip it")
    }
    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", converted) {
        t.Fatalf("expected the converted value to be searchable via CiphertextCandidates")
    }

    skipped, skipErr := migrator.reencryptTransform(TableSpec{Deterministic: false}, "v2")(randomizedUnderTarget)
    if nil != skipErr {
        t.Fatalf("randomized reencrypt transform: %v", skipErr)
    }
    if skipped != randomizedUnderTarget {
        t.Fatalf("expected a randomized same-key reencrypt to keep the fast-path skip")
    }
}

func TestReencryptTransform_RandomizedSameKeyRewritesDeterministicValue(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    deterministicUnderTarget, _ := cipher.EncryptDeterministicWithKeyId("alice@example.com", "v2")
    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", deterministicUnderTarget) {
        t.Fatalf("precondition: deterministic value should be searchable")
    }

    rewritten, rewriteErr := migrator.reencryptTransform(TableSpec{Deterministic: false}, "v2")(deterministicUnderTarget)
    if nil != rewriteErr {
        t.Fatalf("randomized reencrypt transform: %v", rewriteErr)
    }
    if rewritten == deterministicUnderTarget {
        t.Fatalf("expected a randomized reencrypt to rewrite a deterministic same-key value, but it was skipped")
    }
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", rewritten) {
        t.Fatalf("expected the rewritten value to no longer be searchable via CiphertextCandidates")
    }
    if plaintext, _ := cipher.Decrypt(rewritten); "alice@example.com" != plaintext {
        t.Fatalf("expected the rewritten value to still decrypt to the original plaintext")
    }
}

type stubMigrateDriver struct {
    rowsAffected int64
    served       bool
    mutex        sync.Mutex
    queryList    []string
}

func (instance *stubMigrateDriver) record(query string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.queryList = append(instance.queryList, query)
}

func (instance *stubMigrateDriver) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string(nil), instance.queryList...)
}

func (instance *stubMigrateDriver) Open(name string) (driver.Conn, error) {
    return &stubMigrateConnection{shared: instance}, nil
}

type stubMigrateConnection struct {
    shared *stubMigrateDriver
}

func (instance *stubMigrateConnection) Prepare(query string) (driver.Stmt, error) {
    instance.shared.record(query)

    return &stubMigrateStatement{shared: instance.shared, query: query}, nil
}

func (instance *stubMigrateConnection) Close() error {
    return nil
}

func (instance *stubMigrateConnection) Begin() (driver.Tx, error) {
    return nil, errors.New("transactions are not supported by the stub")
}

type stubMigrateStatement struct {
    shared *stubMigrateDriver
    query  string
}

func (instance *stubMigrateStatement) Close() error {
    return nil
}

func (instance *stubMigrateStatement) NumInput() int {
    return -1
}

func (instance *stubMigrateStatement) Exec(arguments []driver.Value) (driver.Result, error) {
    return &stubMigrateResult{rowsAffected: instance.shared.rowsAffected}, nil
}

func (instance *stubMigrateStatement) Query(arguments []driver.Value) (driver.Rows, error) {
    /* the capacity pre-flight probes the widest value not already sealed; the stub answers "no such value" so the probe returns a row rather than no rows at all */
    if true == strings.Contains(instance.query, "MAX(LENGTH(") {
        return &stubMigrateRows{columns: []string{"longest"}, remaining: [][]driver.Value{{nil}}}, nil
    }

    if false == strings.Contains(instance.query, "ORDER BY") || true == instance.shared.served {
        return &stubMigrateRows{}, nil
    }

    instance.shared.served = true

    return &stubMigrateRows{remaining: [][]driver.Value{{"row-1", "plaintext"}}}, nil
}

type stubMigrateResult struct {
    rowsAffected int64
}

func (instance *stubMigrateResult) LastInsertId() (int64, error) {
    return 0, nil
}

func (instance *stubMigrateResult) RowsAffected() (int64, error) {
    return instance.rowsAffected, nil
}

type stubMigrateRows struct {
    columns   []string
    remaining [][]driver.Value
}

func (instance *stubMigrateRows) Columns() []string {
    if 0 != len(instance.columns) {
        return instance.columns
    }

    return []string{"id", "secret"}
}

func (instance *stubMigrateRows) Close() error {
    return nil
}

func (instance *stubMigrateRows) Next(destination []driver.Value) error {
    if 0 == len(instance.remaining) {
        return io.EOF
    }

    row := instance.remaining[0]
    instance.remaining = instance.remaining[1:]

    copy(destination, row)

    return nil
}

func newStubMigrator(t *testing.T, driverName string, rowsAffected int64) *Migrator {
    t.Helper()

    /* the registration carries a per-call sequence suffix: sql.Register panics on a repeated name, so a fixed name held every one of these tests to a single run and go test -count=N died on the second repetition */
    driverName = fmt.Sprintf("%s-%d", driverName, scriptedSqlSequence.Add(1))
    sql.Register(driverName, &stubMigrateDriver{rowsAffected: rowsAffected})

    sqlDatabase, openErr := sql.Open(driverName, "stub")
    if nil != openErr {
        t.Fatalf("open stub database: %v", openErr)
    }

    t.Cleanup(func() { _ = sqlDatabase.Close() })

    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})

    return &Migrator{
        db:     bun.NewDB(sqlDatabase, mysqldialect.New()),
        cipher: NewCipher(provider),
    }
}

func newRecordingStubMigrator(t *testing.T, driverName string, rowsAffected int64) (*Migrator, *stubMigrateDriver) {
    t.Helper()

    stub := &stubMigrateDriver{rowsAffected: rowsAffected}

    /* the same per-call sequence suffix as newStubMigrator, for the same repeated-run reason */
    driverName = fmt.Sprintf("%s-%d", driverName, scriptedSqlSequence.Add(1))
    sql.Register(driverName, stub)

    sqlDatabase, openErr := sql.Open(driverName, "stub")
    if nil != openErr {
        t.Fatalf("open stub database: %v", openErr)
    }

    t.Cleanup(func() { _ = sqlDatabase.Close() })

    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})

    return &Migrator{
        db:     bun.NewDB(sqlDatabase, mysqldialect.New()),
        cipher: NewCipher(provider),
    }, stub
}

/* the update is guarded on the value read for the row, so a row that changed under the run matches zero rows and keeps its plaintext; counting it as processed and exiting zero let a deployment gated on the exit code proceed over values that were never migrated */
func TestMigrate_ReportsRowsTheGuardedUpdateDidNotTouch(t *testing.T) {
    migrator := newStubMigrator(t, "zzMigrateSkipStub", 0)

    processed, runErr := migrator.MigrateEncrypt(
        context.Background(),
        TableSpec{Table: "user", PrimaryKey: "id", Columns: []string{"secret"}},
    )

    if nil == runErr {
        t.Fatalf("expected a skipped row to be reported, got processed=%d and no error", processed)
    }

    if false == strings.Contains(runErr.Error(), "untouched") {
        t.Fatalf("unexpected error: %v", runErr)
    }

    if 0 != processed {
        t.Fatalf("expected the skipped row not to be counted as processed, got %d", processed)
    }
}

func TestMigrate_CountsRowsTheUpdateApplied(t *testing.T) {
    migrator := newStubMigrator(t, "zzMigrateAppliedStub", 1)

    processed, runErr := migrator.MigrateEncrypt(
        context.Background(),
        TableSpec{Table: "user", PrimaryKey: "id", Columns: []string{"secret"}},
    )

    if nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    if 1 != processed {
        t.Fatalf("expected the applied row to be counted, got %d", processed)
    }
}

/* the pre-image guard is evaluated under the COLUMN's collation unless the comparison says otherwise, and every collation the migrator meets in practice equates values it exists to tell apart: MySQL 8's default utf8mb4_0900_ai_ci ignores casing, and any PAD SPACE collation ignores trailing whitespace. A concurrent write of either shape then matches the value that was read, the update applies, that write is destroyed and no skip is recorded. */
func TestApplyRow_GuardsThePreImageOnBytesRatherThanTheColumnCollation(t *testing.T) {
    migrator, stub := newRecordingStubMigrator(t, "zzMigrateGuardShapeStub", 1)

    if _, runErr := migrator.MigrateEncrypt(
        context.Background(),
        TableSpec{Table: "user", PrimaryKey: "id", Columns: []string{"secret"}},
    ); nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    var updateSql string
    for _, query := range stub.recorded() {
        if true == strings.HasPrefix(query, "UPDATE ") {
            updateSql = query
        }
    }

    if "" == updateSql {
        t.Fatalf("expected the run to issue an update, recorded %v", stub.recorded())
    }

    if false == strings.Contains(updateSql, "CAST(`secret` AS BINARY) = ?") {
        t.Fatalf("expected the pre-image guard to compare bytes, got %q", updateSql)
    }

    if true == strings.Contains(updateSql, "AND `secret` = ?") {
        t.Fatalf("expected no collation-dependent pre-image comparison, got %q", updateSql)
    }
}

/* a seal expands its input by roughly a third plus fifty characters, so an in-place encryption needs a wider column than the plaintext ever did. The numbers the capacity check and the README quote are pinned here against real seals: a VARCHAR(255) holds the sealed form of 152 bytes and overflows at 153. */
func TestSealedProbeLength_PinsTheExpansionAVarchar255StopsFitting(t *testing.T) {
    migrator := &Migrator{cipher: NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)}))}

    cases := []struct {
        plaintextByteLength int
        expected            int
    }{
        /* 11 marker characters, a two character key id, a colon, and the base64 of a 12 byte nonce plus the plaintext plus a 16 byte tag */
        {plaintextByteLength: 0, expected: 52},
        {plaintextByteLength: 152, expected: 254},
        {plaintextByteLength: 153, expected: 256},
        {plaintextByteLength: 200, expected: 318},
    }

    for _, testCase := range cases {
        measured, measureErr := migrator.sealedProbeLength(testCase.plaintextByteLength, "")
        if nil != measureErr {
            t.Fatalf("%d bytes: %v", testCase.plaintextByteLength, measureErr)
        }

        if testCase.expected != measured {
            t.Fatalf("expected %d plaintext bytes to seal to %d characters, got %d", testCase.plaintextByteLength, testCase.expected, measured)
        }
    }
}

/* a rotation seals under the key it is rotating TO, and the key id is stored in the value, so a measurement taken under the current key reports a width the run then overflows by exactly the difference between the two ids. */
func TestSealedProbeLength_MeasuresUnderTheKeyIdTheRunWillSealWith(t *testing.T) {
    migrator := &Migrator{cipher: NewCipher(NewStaticKeyProvider(
        "v1",
        map[string][]byte{"v1": newKey(1), "2026-07-rotated": newKey(2)},
    ))}

    current, currentErr := migrator.sealedProbeLength(100, "")
    if nil != currentErr {
        t.Fatalf("measure under the current key: %v", currentErr)
    }

    rotated, rotatedErr := migrator.sealedProbeLength(100, "2026-07-rotated")
    if nil != rotatedErr {
        t.Fatalf("measure under the target key: %v", rotatedErr)
    }

    if current+len("2026-07-rotated")-len("v1") != rotated {
        t.Fatalf(
            "expected the target key id to widen the measurement by the difference in key id length, got %d under %q and %d under %q",
            current,
            "v1",
            rotated,
            "2026-07-rotated",
        )
    }
}

/* an already-sealed value is handed back unchanged and is already stored in the column, so it must never be measured as a plaintext that still has to grow — otherwise a second run over a migrated column would demand a column wide enough to seal the ciphertext. */
func TestLongestUnsealedLength_ExcludesValuesThatAreAlreadySealed(t *testing.T) {
    migrator, stub := newRecordingStubMigrator(t, "zzMigrateUnsealedProbeStub", 1)

    _, hasUnsealed, probeErr := migrator.longestUnsealedLength(context.Background(), "user", "secret")
    if nil != probeErr {
        t.Fatalf("unexpected error: %v", probeErr)
    }

    if true == hasUnsealed {
        t.Fatalf("expected a column of already-sealed values to report nothing left to seal")
    }

    var probeSql string
    for _, query := range stub.recorded() {
        if true == strings.Contains(query, "MAX(LENGTH(") {
            probeSql = query
        }
    }

    if "" == probeSql {
        t.Fatalf("expected a length probe query, recorded %v", stub.recorded())
    }

    if false == strings.Contains(probeSql, "CAST(`secret` AS BINARY) NOT LIKE ?") {
        t.Fatalf("expected the probe to exclude already-sealed values byte-exactly, got %q", probeSql)
    }
}

/* The width is computed from a probe of at most two bytes plus arithmetic, and it has to agree with what the cipher actually emits for every plaintext length — not on average, exactly, since the number decides whether a column is declared wide enough. Sealing the full length to find out was the old way and is unusable at the widths this is asked about: `longest` comes from SELECT MAX(LENGTH(col)), so a 64 MiB row cost hundreds of megabytes resident, and LONGTEXT reaches 4 GiB. Every residue class mod three is covered, together with the boundaries where base64 rounds. */
func TestSealedProbeLength_AgreesWithASealOfTheFullPlaintext(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    for _, keyId := range []string{"", "v1", "v2"} {
        for plaintextByteLength := 0; plaintextByteLength <= 64; plaintextByteLength = plaintextByteLength + 1 {
            computed, computedErr := migrator.sealedProbeLength(plaintextByteLength, keyId)
            if nil != computedErr {
                t.Fatalf("key %q length %d: %v", keyId, plaintextByteLength, computedErr)
            }

            fullProbe := strings.Repeat(sealedProbeFiller, plaintextByteLength)

            var sealed string
            var sealErr error
            if "" == keyId {
                sealed, sealErr = cipher.Encrypt(fullProbe)
            } else {
                sealed, sealErr = cipher.EncryptWithKeyId(fullProbe, keyId)
            }
            if nil != sealErr {
                t.Fatalf("key %q length %d: sealing the full probe: %v", keyId, plaintextByteLength, sealErr)
            }

            if len(sealed) != computed {
                t.Fatalf(
                    "key %q length %d: the computed width is %d and the cipher emits %d",
                    keyId,
                    plaintextByteLength,
                    computed,
                    len(sealed),
                )
            }
        }
    }
}

/* the point of the rewrite: a width that a column could actually hold must be answerable without allocating it. LONGTEXT reaches 4 GiB, and the old measurement sealed the whole thing. */
func TestSealedProbeLength_AnswersAHugeWidthWithoutAllocatingIt(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v2": newKey(2)})
    migrator := &Migrator{cipher: NewCipher(provider)}

    computed, computedErr := migrator.sealedProbeLength(512*1024*1024, "")
    if nil != computedErr {
        t.Fatalf("unexpected error: %v", computedErr)
    }

    if computed <= 512*1024*1024 {
        t.Fatalf("a sealed value is wider than its plaintext; got %d for 512 MiB", computed)
    }
}

/* the TEXT family enforces its limit in BYTES while CHARACTER_MAXIMUM_LENGTH reports the worst-case character count; reading the character count as the capacity refused ASCII migrations that fit with room to spare */
func TestColumnWidth_ReadsTheTextFamilyInBytes(t *testing.T) {
    migrator, _ := newScriptedMigrator(t, []scriptedSqlResponse{
        {
            fragment: "information_schema.COLUMNS",
            columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
            rows:     [][]driver.Value{{"text", int64(16383), int64(65535)}},
        },
    })

    width, widthErr := migrator.columnWidth(context.Background(), "accounts", "iban")
    if nil != widthErr {
        t.Fatalf("width: %v", widthErr)
    }

    if 65535 != width {
        t.Fatalf("expected the byte capacity for a text column, got %d", width)
    }
}

func TestColumnWidth_ReadsAVarcharInCharacters(t *testing.T) {
    migrator, _ := newScriptedMigrator(t, []scriptedSqlResponse{
        {
            fragment: "information_schema.COLUMNS",
            columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
            rows:     [][]driver.Value{{"varchar", int64(255), int64(1020)}},
        },
    })

    width, widthErr := migrator.columnWidth(context.Background(), "accounts", "iban")
    if nil != widthErr {
        t.Fatalf("width: %v", widthErr)
    }

    if 255 != width {
        t.Fatalf("expected the character capacity for a varchar column, got %d", width)
    }
}

/* the capacity check used to live only in the CLI command, so the programmatic caller ran with none at all */
func TestMigrateEncrypt_RefusesANarrowColumnBeforeWritingARow(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
        {
            fragment: "NOT LIKE",
            columns:  []string{"longest"},
            rows:     [][]driver.Value{{int64(300)}},
        },
        {
            fragment: "information_schema.COLUMNS",
            columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
            rows:     [][]driver.Value{{"varchar", int64(255), int64(1020)}},
        },
    })

    _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"iban"}})
    if nil == runErr {
        t.Fatalf("expected the narrow column to refuse the run")
    }

    if false == strings.Contains(runErr.Error(), "too narrow") {
        t.Fatalf("expected the width refusal, got: %v", runErr)
    }

    for _, query := range stub.recorded() {
        if true == strings.Contains(query, "ORDER BY") {
            t.Fatalf("expected the refusal to land before the first page was read, saw: %s", query)
        }
    }
}

/* rendered as "", a NULL cursor value coerces the next page's keyset predicate to pk > 0 on an integer column — the first-page hazard one page later */
func TestMigrateRun_RefusesANullPrimaryKeyCursor(t *testing.T) {
    migrator, _ := newScriptedMigrator(t, []scriptedSqlResponse{
        {
            fragment: "NOT LIKE",
            columns:  []string{"longest"},
            rows:     [][]driver.Value{{nil}},
        },
        {
            fragment: "ORDER BY",
            columns:  []string{"id", "iban"},
            rows:     [][]driver.Value{{nil, "plaintext"}},
        },
    })

    _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"iban"}})
    if nil == runErr {
        t.Fatalf("expected the NULL cursor value to stop the run")
    }

    if false == strings.Contains(runErr.Error(), "NULL primary key") {
        t.Fatalf("expected the cursor refusal, got: %v", runErr)
    }
}

/* the keyset cursor of an integer key is bound as the integer itself: bound as its text, MySQL compares the integer column against the string parameter as double-precision floats, where keys at and above 2^53 collapse onto shared doubles — the next page silently skips the rows that round onto the cursor and the update guard can match a neighbouring key. */
func TestMigrateRun_BindsAnIntegerPrimaryKeyCursorAsAnInteger(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
        {
            fragment: "NOT LIKE",
            columns:  []string{"longest"},
            rows:     [][]driver.Value{{int64(10)}},
        },
        {
            fragment: "information_schema.COLUMNS",
            columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
            rows:     [][]driver.Value{{"varchar", int64(4096), int64(16384)}},
        },
        {
            fragment:    "WHERE",
            columns:     []string{"id", "iban"},
            columnTypes: []string{"BIGINT", "VARCHAR"},
            rows:        nil,
        },
        {
            fragment:    "ORDER BY",
            columns:     []string{"id", "iban"},
            columnTypes: []string{"BIGINT", "VARCHAR"},
            rows:        [][]driver.Value{{"9007199254740993", "plaintext"}},
        },
    })

    _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"iban"}, BatchSize: 1})
    if nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    keysetArguments := ([]driver.Value)(nil)
    for _, recorded := range stub.recordedArguments() {
        if true == strings.Contains(recorded.query, "WHERE") && true == strings.Contains(recorded.query, "ORDER BY") {
            keysetArguments = recorded.arguments
        }
    }

    if nil == keysetArguments || 0 == len(keysetArguments) {
        t.Fatalf("expected the second page's keyset query to have been recorded, got %+v", stub.recordedArguments())
    }

    if int64(9007199254740993) != keysetArguments[0] {
        t.Fatalf("expected the cursor bound as the integer itself, got %T %v", keysetArguments[0], keysetArguments[0])
    }
}

func TestTypedPrimaryKeyArgument_ConvertsByTheColumnType(t *testing.T) {
    if int64(42) != typedPrimaryKeyArgument("42", "BIGINT") {
        t.Fatalf("expected a signed integer column bound as int64")
    }

    if uint64(18446744073709551615) != typedPrimaryKeyArgument("18446744073709551615", "UNSIGNED BIGINT") {
        t.Fatalf("expected an unsigned value past MaxInt64 bound as uint64")
    }

    if "42" != typedPrimaryKeyArgument("42", "VARCHAR") {
        t.Fatalf("expected a string column to keep the string binding")
    }

    if "42" != typedPrimaryKeyArgument("42", "") {
        t.Fatalf("expected a driver that reports no type to keep the string binding")
    }

    if int64(7) != typedPrimaryKeyArgument("7", "INT8") {
        t.Fatalf("expected the postgres integer spelling to bind as int64")
    }
}

/* a SIGTERM mid-bulk used to surface as "migrate select failed", indistinguishable from a broken column */
func TestClassifyRunError_NamesAnInterruptedRun(t *testing.T) {
    migrator := &Migrator{}

    classified := migrator.classifyRunError(TableSpec{Table: "accounts"}, 7, "migrate select failed", fmt.Errorf("query: %w", context.Canceled))

    if false == strings.Contains(classified.Error(), "interrupted") {
        t.Fatalf("expected the interruption to name itself, got: %v", classified)
    }

    logContext := exception.LogContext(classified)
    if 7 != logContext["processed"] {
        t.Fatalf("expected the interruption to carry how far the run got, got: %v", logContext["processed"])
    }
}

type normalisingCipher struct {
    Cipher

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
            Cipher:      NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newRampKey()})),
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

        migrator := NewMigrator(database, cipher)

        processed, runErr := migrator.MigrateEncrypt(ctx, TableSpec{
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

    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newRampKey()}))
    migrator := NewMigrator(database, cipher)

    spec := TableSpec{Table: table, PrimaryKey: "id", Columns: []string{"secret"}}

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

    cipher := NewCipher(NewStaticKeyProvider(
        "v1",
        map[string][]byte{"v1": newRampKey(), targetKeyId: rotationKey},
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

    migrator := NewMigrator(database, cipher)

    spec := TableSpec{Table: table, PrimaryKey: "id", Columns: []string{"secret"}}

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
