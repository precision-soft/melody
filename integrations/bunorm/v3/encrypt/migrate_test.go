package encrypt

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "sync"
    "testing"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
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

/* a marker-shaped value that authenticates under no key in the set is ordinary plaintext, and the cipher seals it rather than storing it verbatim; the conversion path must not turn that into a run-killing decrypt error. */
func TestEncryptTransform_DeterministicSealsAMarkerShapedPlaintext(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v2": newKey(2)})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    foreign, foreignErr := NewCipher(NewStaticKeyProvider("v9", map[string][]byte{"v9": newKey(9)})).Encrypt("alice@example.com")
    if nil != foreignErr {
        t.Fatalf("foreign encrypt: %v", foreignErr)
    }

    converted, convertErr := migrator.encryptTransform(TableSpec{Deterministic: true})(foreign)
    if nil != convertErr {
        t.Fatalf("deterministic encrypt transform: %v", convertErr)
    }

    if false == deterministicCandidateMatches(t, cipher, foreign, converted) {
        t.Fatalf("expected the unauthenticated value to be sealed as plaintext under the current key")
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

/* @info the update is guarded on the value read for the row, so a row that changed under the run matches zero rows and keeps its plaintext; counting it as processed and exiting zero let a deployment gated on the exit code proceed over values that were never migrated */
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
