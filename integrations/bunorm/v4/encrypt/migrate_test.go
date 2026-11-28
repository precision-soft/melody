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
    "time"

    _ "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

func TestEncryptTransform_DeterministicProducesSearchableCiphertext(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    deterministic, transformErr := migrator.encryptTransform(TableSpecification{Deterministic: true})("alice@example.com")
    if nil != transformErr {
        t.Fatalf("deterministic encrypt transform: %v", transformErr)
    }
    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", deterministic) {
        t.Fatalf("expected the deterministic encrypt transform to be searchable via CiphertextCandidates")
    }

    randomized, randomizedErr := migrator.encryptTransform(TableSpecification{Deterministic: false})("alice@example.com")
    if nil != randomizedErr {
        t.Fatalf("randomized encrypt transform: %v", randomizedErr)
    }
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", randomized) {
        t.Fatalf("expected the randomized encrypt transform to not match deterministic candidates")
    }
}

func TestEncryptTransform_DeterministicConvertsAnAlreadyRandomizedValue(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    randomized, randomizedErr := cipher.Encrypt("alice@example.com", EncryptOptions{KeyId: "v2"})
    if nil != randomizedErr {
        t.Fatalf("randomized encrypt: %v", randomizedErr)
    }
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", randomized) {
        t.Fatalf("precondition: a randomized value must not be searchable")
    }

    converted, convertErr := migrator.encryptTransform(TableSpecification{Deterministic: true})(randomized)
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

func TestEncryptTransform_DeterministicKeepsTheKeyTheValueCarries(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    randomizedUnderRetiredKey, randomizedErr := cipher.Encrypt("alice@example.com", EncryptOptions{KeyId: "v1"})
    if nil != randomizedErr {
        t.Fatalf("randomized encrypt: %v", randomizedErr)
    }

    converted, convertErr := migrator.encryptTransform(TableSpecification{Deterministic: true})(randomizedUnderRetiredKey)
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

func TestEncryptTransform_DeterministicIsIdempotent(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    transform := migrator.encryptTransform(TableSpecification{Deterministic: true})

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

func TestEncryptTransform_StopsOnAStoredValueThatNoLongerDecrypts(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v2": newKey(2)}})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    foreign, foreignErr := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v9", Keys: map[string][]byte{"v9": newKey(9)}})).Encrypt("alice@example.com", EncryptOptions{})
    if nil != foreignErr {
        t.Fatalf("foreign encrypt: %v", foreignErr)
    }

    for _, deterministic := range []bool{false, true} {
        _, convertErr := migrator.encryptTransform(TableSpecification{Deterministic: deterministic})(foreign)
        if nil == convertErr {
            t.Fatalf("expected the stored undecryptable value to stop the run (deterministic=%v)", deterministic)
        }

        if false == strings.Contains(convertErr.Error(), "no longer decrypts") {
            t.Fatalf("expected the refusal to name the undecryptable value, got: %v", convertErr)
        }
    }
}

func TestReencryptTransform_ConvertsRandomizedSameKeyValueToDeterministic(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    randomizedUnderTarget, _ := cipher.Encrypt("alice@example.com", EncryptOptions{KeyId: "v2"})
    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", randomizedUnderTarget) {
        t.Fatalf("precondition: randomized value should not be searchable")
    }

    converted, convertErr := migrator.reencryptTransform(TableSpecification{Deterministic: true}, "v2")(randomizedUnderTarget)
    if nil != convertErr {
        t.Fatalf("deterministic reencrypt transform: %v", convertErr)
    }
    if converted == randomizedUnderTarget {
        t.Fatalf("expected a deterministic reencrypt to convert a randomized same-key value rather than skip it")
    }
    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", converted) {
        t.Fatalf("expected the converted value to be searchable via CiphertextCandidates")
    }

    skipped, skipErr := migrator.reencryptTransform(TableSpecification{Deterministic: false}, "v2")(randomizedUnderTarget)
    if nil != skipErr {
        t.Fatalf("randomized reencrypt transform: %v", skipErr)
    }
    if skipped != randomizedUnderTarget {
        t.Fatalf("expected a randomized same-key reencrypt to keep the fast-path skip")
    }
}

func TestReencryptTransform_RandomizedSameKeyRewritesDeterministicValue(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
    cipher := NewCipher(provider)
    migrator := &Migrator{cipher: cipher}

    deterministicUnderTarget, _ := cipher.EncryptDeterministic("alice@example.com", EncryptOptions{KeyId: "v2"})
    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", deterministicUnderTarget) {
        t.Fatalf("precondition: deterministic value should be searchable")
    }

    rewritten, rewriteErr := migrator.reencryptTransform(TableSpecification{Deterministic: false}, "v2")(deterministicUnderTarget)
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

func TestReencryptTransform_AKeyChangeKeepsADeterministicSealDeterministic(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}}))
    migrator := &Migrator{cipher: cipher}

    deterministicUnderV1, _ := cipher.EncryptDeterministic("alice@example.com", EncryptOptions{KeyId: "v1"})

    rotated, rotateErr := migrator.reencryptTransform(TableSpecification{Deterministic: false}, "v2")(deterministicUnderV1)
    if nil != rotateErr {
        t.Fatalf("reencrypt transform: %v", rotateErr)
    }

    if keyId, _, _ := keyIdOf(rotated); "v2" != keyId {
        t.Fatalf("expected the value rotated onto v2, got %q", keyId)
    }

    if false == deterministicCandidateMatches(t, cipher, "alice@example.com", rotated) {
        t.Fatal("expected the rotated value still searchable via CiphertextCandidates")
    }
}

func TestReencryptTransform_AKeyChangeKeepsARandomSealRandom(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}}))
    migrator := &Migrator{cipher: cipher}

    randomUnderV1, _ := cipher.Encrypt("alice@example.com", EncryptOptions{KeyId: "v1"})

    rotated, rotateErr := migrator.reencryptTransform(TableSpecification{Deterministic: false}, "v2")(randomUnderV1)
    if nil != rotateErr {
        t.Fatalf("reencrypt transform: %v", rotateErr)
    }

    if keyId, _, _ := keyIdOf(rotated); "v2" != keyId {
        t.Fatalf("expected the value rotated onto v2, got %q", keyId)
    }

    if true == deterministicCandidateMatches(t, cipher, "alice@example.com", rotated) {
        t.Fatal("expected the rotated value to stay random")
    }

    if plaintext, _ := cipher.Decrypt(rotated); "alice@example.com" != plaintext {
        t.Fatalf("expected the rotated value to decrypt to the original plaintext, got %q", plaintext)
    }
}

func TestReencryptTransform_AKeyChangeThroughAForeignCipherKeepsTheMode(t *testing.T) {
    packageCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}}))
    migrator := &Migrator{cipher: wrappedCipher{Cipher: packageCipher}}

    deterministicUnderV1, _ := packageCipher.EncryptDeterministic("alice@example.com", EncryptOptions{KeyId: "v1"})
    randomUnderV1, _ := packageCipher.Encrypt("alice@example.com", EncryptOptions{KeyId: "v1"})

    for _, testCase := range []struct {
        stored        string
        deterministic bool
    }{
        {stored: deterministicUnderV1, deterministic: true},
        {stored: randomUnderV1, deterministic: false},
    } {
        rotated, rotateErr := migrator.reencryptTransform(TableSpecification{Deterministic: false}, "v2")(testCase.stored)
        if nil != rotateErr {
            t.Fatalf("reencrypt transform (deterministic=%v): %v", testCase.deterministic, rotateErr)
        }

        if keyId, _, _ := keyIdOf(rotated); "v2" != keyId {
            t.Fatalf("expected the value rotated onto v2 (deterministic=%v), got %q", testCase.deterministic, keyId)
        }

        if testCase.deterministic != deterministicCandidateMatches(t, packageCipher, "alice@example.com", rotated) {
            t.Fatalf("expected the rotated value's mode kept (deterministic=%v)", testCase.deterministic)
        }
    }
}

func TestMigrator_AForeignCipherSealsUnderTheKeyTheRunNamesNotTheCurrentOne(t *testing.T) {
    packageCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2), "v3": newKey(3)}}))
    migrator := &Migrator{cipher: wrappedCipher{Cipher: packageCipher}}

    randomUnderV2, _ := packageCipher.Encrypt("alice@example.com", EncryptOptions{KeyId: "v2"})
    deterministicUnderV2, _ := packageCipher.EncryptDeterministic("alice@example.com", EncryptOptions{KeyId: "v2"})
    deterministicUnderV3, _ := packageCipher.EncryptDeterministic("alice@example.com", EncryptOptions{KeyId: "v3"})

    for _, testCase := range []struct {
        name          string
        transform     func(string) (string, error)
        stored        string
        keyId         string
        deterministic bool
    }{
        {name: "reencrypt keeps a random seal random", transform: migrator.reencryptTransform(TableSpecification{}, "v3"), stored: randomUnderV2, keyId: "v3", deterministic: false},
        {name: "reencrypt converts to deterministic", transform: migrator.reencryptTransform(TableSpecification{Deterministic: true}, "v3"), stored: randomUnderV2, keyId: "v3", deterministic: true},
        {name: "reencrypt onto the seal's own key converts to random", transform: migrator.reencryptTransform(TableSpecification{}, "v3"), stored: deterministicUnderV3, keyId: "v3", deterministic: false},
        {name: "encrypt converts a random seal under its own key", transform: migrator.encryptTransform(TableSpecification{Deterministic: true}), stored: randomUnderV2, keyId: "v2", deterministic: true},
        {name: "reencrypt keeps a deterministic seal deterministic", transform: migrator.reencryptTransform(TableSpecification{}, "v3"), stored: deterministicUnderV2, keyId: "v3", deterministic: true},
    } {
        sealed, sealErr := testCase.transform(testCase.stored)
        if nil != sealErr {
            t.Fatalf("%s: %v", testCase.name, sealErr)
        }

        if keyId, _, _ := keyIdOf(sealed); testCase.keyId != keyId {
            t.Fatalf("%s: expected the seal under %q, got %q", testCase.name, testCase.keyId, keyId)
        }

        if plaintext, decryptErr := packageCipher.Decrypt(sealed); nil != decryptErr || "alice@example.com" != plaintext {
            t.Fatalf("%s: expected the seal to open to the plaintext, got %q (%v)", testCase.name, plaintext, decryptErr)
        }

        if testCase.deterministic != (sealed == mustDeterministicUnder(t, packageCipher, testCase.keyId)) {
            t.Fatalf("%s: expected deterministic=%v", testCase.name, testCase.deterministic)
        }
    }
}

func mustDeterministicUnder(t *testing.T, cipherInstance Cipher, keyId string) string {
    t.Helper()

    sealed, sealErr := cipherInstance.EncryptDeterministic("alice@example.com", EncryptOptions{KeyId: keyId})
    if nil != sealErr {
        t.Fatalf("deterministic seal under %q: %v", keyId, sealErr)
    }

    return sealed
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
    if true == strings.Contains(instance.query, "IS_NULLABLE") {
        return &stubMigrateRows{columns: []string{"COLUMN_NAME", "IS_NULLABLE"}, remaining: [][]driver.Value{{"id", "NO"}, {"secret", "NO"}}}, nil
    }

    if true == strings.Contains(instance.query, "STATISTICS") {
        return &stubMigrateRows{columns: []string{"INDEX_NAME", "COLUMN_NAME", "NON_UNIQUE"}, remaining: [][]driver.Value{{"PRIMARY", "id", int64(0)}}}, nil
    }

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

    /* a per-call suffix: sql.Register panics on a repeated name, which killed go test -count=N */
    driverName = fmt.Sprintf("%s-%d", driverName, scriptedSqlSequence.Add(1))
    sql.Register(driverName, &stubMigrateDriver{rowsAffected: rowsAffected})

    sqlDatabase, openErr := sql.Open(driverName, "stub")
    if nil != openErr {
        t.Fatalf("open stub database: %v", openErr)
    }

    t.Cleanup(func() { _ = sqlDatabase.Close() })

    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})

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

    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})

    return &Migrator{
        db:     bun.NewDB(sqlDatabase, mysqldialect.New()),
        cipher: NewCipher(provider),
    }, stub
}

func TestMigrate_ReportsRowsTheGuardedUpdateDidNotTouch(t *testing.T) {
    migrator := newStubMigrator(t, "zzMigrateSkipStub", 0)

    processed, runErr := migrator.MigrateEncrypt(
        testRuntime(),
        TableSpecification{Table: "user", PrimaryKey: "id", Columns: []string{"secret"}},
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
        testRuntime(),
        TableSpecification{Table: "user", PrimaryKey: "id", Columns: []string{"secret"}},
    )

    if nil != runErr {
        t.Fatalf("unexpected error: %v", runErr)
    }

    if 1 != processed {
        t.Fatalf("expected the applied row to be counted, got %d", processed)
    }
}

func TestApplyRow_GuardsThePreImageOnBytesRatherThanTheColumnCollation(t *testing.T) {
    migrator, stub := newRecordingStubMigrator(t, "zzMigrateGuardShapeStub", 1)

    if _, runErr := migrator.MigrateEncrypt(
        testRuntime(),
        TableSpecification{Table: "user", PrimaryKey: "id", Columns: []string{"secret"}},
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

func TestSealedProbeLength_PinsTheExpansionAVarchar255StopsFitting(t *testing.T) {
    migrator := &Migrator{cipher: NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))}

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

func TestSealedProbeLength_MeasuresUnderTheKeyIdTheRunWillSealWith(t *testing.T) {
    migrator := &Migrator{cipher: NewCipher(NewStaticKeyProvider(KeyRing{
        CurrentKeyId: "v1",
        Keys:         map[string][]byte{"v1": newKey(1), "2026-07-rotated": newKey(2)},
    }))}

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

func TestLongestUnsealedLength_ExcludesValuesThatAreAlreadySealed(t *testing.T) {
    migrator, stub := newRecordingStubMigrator(t, "zzMigrateUnsealedProbeStub", 1)

    _, hasUnsealed, probeErr := migrator.longestUnsealedLength(context.Background(), "user", "secret", "")
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

func TestSealedProbeLength_AgreesWithASealOfTheFullPlaintext(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}})
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
                sealed, sealErr = cipher.Encrypt(fullProbe, EncryptOptions{})
            } else {
                sealed, sealErr = cipher.Encrypt(fullProbe, EncryptOptions{KeyId: keyId})
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

func TestSealedProbeLength_AnswersAHugeWidthWithoutAllocatingIt(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v2": newKey(2)}})
    migrator := &Migrator{cipher: NewCipher(provider)}

    computed, computedErr := migrator.sealedProbeLength(512*1024*1024, "")
    if nil != computedErr {
        t.Fatalf("unexpected error: %v", computedErr)
    }

    if computed <= 512*1024*1024 {
        t.Fatalf("a sealed value is wider than its plaintext; got %d for 512 MiB", computed)
    }
}

func TestMigrator_SizesTheColumnUnderTheTargetKey(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1), "rotation-target-v2": newKey(2)}})
    migrator := &Migrator{cipher: NewCipher(provider)}

    for keyId, expectedKeyId := range map[string]string{"rotation-target-v2": "rotation-target-v2", "": "v1"} {
        computed, computedErr := migrator.sealedProbeLength(10, keyId)
        if nil != computedErr {
            t.Fatalf("key %q: unexpected error: %v", keyId, computedErr)
        }

        sealed, sealErr := migrator.cipher.Encrypt(strings.Repeat(sealedProbeFiller, 10), EncryptOptions{KeyId: expectedKeyId})
        if nil != sealErr {
            t.Fatalf("key %q: seal: %v", keyId, sealErr)
        }

        if len(sealed) != computed {
            t.Fatalf("key %q: expected the width of a seal under %q, %d, got %d", keyId, expectedKeyId, len(sealed), computed)
        }
    }
}

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

func TestMigrateEncrypt_RefusesANarrowColumnBeforeWritingARow(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
        {fragment: "IS_NULLABLE", columns: []string{"COLUMN_NAME", "IS_NULLABLE"}, rows: [][]driver.Value{{"id", "NO"}, {"iban", "NO"}}},
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

    _, runErr := migrator.MigrateEncrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"iban"}})
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

    _, runErr := migrator.MigrateEncrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"iban"}})
    if nil == runErr {
        t.Fatalf("expected the NULL cursor value to stop the run")
    }

    if false == strings.Contains(runErr.Error(), "NULL primary key") {
        t.Fatalf("expected the cursor refusal, got: %v", runErr)
    }
}

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

    _, runErr := migrator.MigrateEncrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"iban"}, BatchSize: bound.Of(1)})
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

func TestClassifyRunError_NamesAnInterruptedRun(t *testing.T) {
    migrator := &Migrator{}

    classified := migrator.classifyRunError(TableSpecification{Table: "accounts"}, 7, "migrate select failed", fmt.Errorf("query: %w", context.Canceled))

    if false == strings.Contains(classified.Error(), "interrupted") {
        t.Fatalf("expected the interruption to name itself, got: %v", classified)
    }

    logContext := exception.LogContext(classified, exception.LogContextOptions{})
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

func (instance *normalisingCipher) Encrypt(plaintext string, options EncryptOptions) (string, error) {
    if plaintext == instance.stored {
        _, execErr := instance.database.ExecContext(
            instance.ctx,
            "UPDATE "+instance.table+" SET "+instance.column+" = "+instance.normalising,
        )
        if nil != execErr {
            instance.updateErr = execErr
        }
    }

    return instance.Cipher.Encrypt(plaintext, options)
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
            Cipher:      NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newRampKey()}})),
            ctx:         ctx,
            database:    database,
            table:       table,
            column:      "email",
            normalising: testCase.normalising,
            stored:      testCase.stored,
        }

        if _, updateErr := database.ExecContext(ctx, "UPDATE "+table+" SET email = ? WHERE id = 1", testCase.stored); nil != updateErr {
            t.Fatalf("%s: reset: %v", testCase.name, updateErr)
        }

        migrator := NewMigrator(database, cipher)

        processed, runErr := migrator.MigrateEncrypt(testRuntimeUnder(ctx), TableSpecification{
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

func TestEncryptMigrate_ACancellationOnTheCharsetLookupIsAnInterruption(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping bunorm encrypt migrate integration test")
    }

    sqlDb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer sqlDb.Close()

    database := bun.NewDB(sqlDb, mysqldialect.New())
    migrator := NewMigrator(database, NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newRampKey()}})))

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    specification := TableSpecification{
        Table:      "migrate_interrupted_record",
        PrimaryKey: "id",
        Columns:    []string{"email"},
    }

    doors := map[string]func() (int, error){
        "encrypt":   func() (int, error) { return migrator.MigrateEncrypt(testRuntimeUnder(cancelledContext), specification) },
        "reencrypt": func() (int, error) { return migrator.MigrateReencrypt(testRuntimeUnder(cancelledContext), specification, "v1") },
        "decrypt":   func() (int, error) { return migrator.MigrateDecrypt(testRuntimeUnder(cancelledContext), specification) },
    }

    for door, run := range doors {
        _, runErr := run()

        var interruption *exception.Error
        if false == errors.As(runErr, &interruption) || false == strings.Contains(interruption.Message(), "interrupted") {
            t.Fatalf("%s: expected the cancelled lookup named an interruption, got %v", door, runErr)
        }

        if "migrate_interrupted_record" != interruption.Context()["table"] || 0 != interruption.Context()["processed"] {
            t.Fatalf("%s: expected the interruption to carry the table and nothing processed, got %v", door, interruption.Context())
        }
    }
}

func TestEncryptMigrate_ACancellationDuringAPageIterationIsAnInterruption(t *testing.T) {
    migrator, _ := newScriptedMigrator(t, []scriptedSqlResponse{
        {
            fragment: "ORDER BY",
            columns:  []string{"id", "email"},
            nextErr:  context.Canceled,
        },
    })

    _, runErr := migrator.MigrateDecrypt(testRuntime(), TableSpecification{
        Table:      "accounts",
        PrimaryKey: "id",
        Columns:    []string{"email"},
    })

    var interruption *exception.Error
    if false == errors.As(runErr, &interruption) || false == strings.Contains(interruption.Message(), "interrupted") {
        t.Fatalf("expected the cancelled iteration named an interruption, got %v", runErr)
    }

    if "accounts" != interruption.Context()["table"] || 0 != interruption.Context()["processed"] || false == errors.Is(runErr, context.Canceled) {
        t.Fatalf("expected the interruption to carry the table, nothing processed and the cancellation, got %v", exception.LogContext(runErr, exception.LogContextOptions{}))
    }
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

    cipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newRampKey()}}))
    migrator := NewMigrator(database, cipher)

    specification := TableSpecification{Table: table, PrimaryKey: "id", Columns: []string{"secret"}}

    capacityErr := migrator.EnsureColumnCapacity(testRuntimeUnder(ctx), specification)
    if nil == capacityErr {
        processed, runErr := migrator.MigrateEncrypt(testRuntimeUnder(ctx), specification)

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

    if _, widenErr := database.ExecContext(ctx, "ALTER TABLE "+table+" MODIFY secret VARCHAR(318) NOT NULL"); nil != widenErr {
        t.Fatalf("widen column: %v", widenErr)
    }

    if recheckErr := migrator.EnsureColumnCapacity(testRuntimeUnder(ctx), specification); nil != recheckErr {
        t.Fatalf("expected the widened column to be accepted, got %v", recheckErr)
    }

    processed, runErr := migrator.MigrateEncrypt(testRuntimeUnder(ctx), specification)
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

    if rerunErr := migrator.EnsureColumnCapacity(testRuntimeUnder(ctx), specification); nil != rerunErr {
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

    cipher := NewCipher(NewStaticKeyProvider(KeyRing{
        CurrentKeyId: "v1",
        Keys:         map[string][]byte{"v1": newRampKey(), targetKeyId: rotationKey},
    }))

    plaintext := strings.Repeat("s", 100)

    sealed, sealErr := cipher.Encrypt(plaintext, EncryptOptions{KeyId: "v1"})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    createSql := "CREATE TABLE " + table + " (" +
        "id BIGINT NOT NULL PRIMARY KEY, " +
        "secret VARCHAR(" + strconv.Itoa(len(sealed)) + ") NOT NULL" +
        ")"
    if _, createErr := database.ExecContext(ctx, createSql); nil != createErr {
        t.Fatalf("create %s: %v", table, createErr)
    }

    /* the driver handle rather than bun: bun renders the string into the statement and drops the marker's nul bytes */
    if _, insertErr := sqlDb.ExecContext(ctx, "INSERT INTO "+table+" (id, secret) VALUES (1, ?)", sealed); nil != insertErr {
        t.Fatalf("insert the sealed row: %v", insertErr)
    }

    shortPlaintext := strings.Repeat("p", 10)
    if _, insertErr := sqlDb.ExecContext(ctx, "INSERT INTO "+table+" (id, secret) VALUES (2, ?)", shortPlaintext); nil != insertErr {
        t.Fatalf("insert the plaintext row: %v", insertErr)
    }

    if _, modeErr := database.ExecContext(ctx, "SET SESSION sql_mode = ''"); nil != modeErr {
        t.Fatalf("relax sql_mode: %v", modeErr)
    }
    defer database.ExecContext(ctx, "SET SESSION sql_mode = DEFAULT")

    migrator := NewMigrator(database, cipher)

    specification := TableSpecification{Table: table, PrimaryKey: "id", Columns: []string{"secret"}}

    capacityErr := migrator.EnsureColumnCapacityForReencrypt(testRuntimeUnder(ctx), specification, targetKeyId)
    if nil == capacityErr {
        processed, runErr := migrator.MigrateReencrypt(testRuntimeUnder(ctx), specification, targetKeyId)

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

    if _, widenErr := database.ExecContext(
        ctx,
        "ALTER TABLE "+table+" MODIFY secret VARCHAR("+strconv.Itoa(requiredWidth)+") NOT NULL",
    ); nil != widenErr {
        t.Fatalf("widen column: %v", widenErr)
    }

    if recheckErr := migrator.EnsureColumnCapacityForReencrypt(testRuntimeUnder(ctx), specification, targetKeyId); nil != recheckErr {
        t.Fatalf("expected the widened column to be accepted, got %v", recheckErr)
    }

    processed, runErr := migrator.MigrateReencrypt(testRuntimeUnder(ctx), specification, targetKeyId)
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

    if rerunErr := migrator.EnsureColumnCapacityForReencrypt(testRuntimeUnder(ctx), specification, targetKeyId); nil != rerunErr {
        t.Fatalf("expected a re-run of the same rotation to be accepted, got %v", rerunErr)
    }
}

type sealStoredAsDataCase struct {
    name             string
    applicationValue string
}

func sealStoredAsDataCases(t *testing.T, cipherInstance Cipher) []sealStoredAsDataCase {
    t.Helper()

    randomSeal, randomErr := cipherInstance.Encrypt("another account's iban", EncryptOptions{})
    if nil != randomErr {
        t.Fatalf("random seal: %v", randomErr)
    }

    deterministicSeal, deterministicErr := cipherInstance.EncryptDeterministic("another account's iban", EncryptOptions{})
    if nil != deterministicErr {
        t.Fatalf("deterministic seal: %v", deterministicErr)
    }

    return []sealStoredAsDataCase{
        {name: "a random seal", applicationValue: randomSeal},
        {name: "a deterministic seal", applicationValue: deterministicSeal},
        {name: "an ordinary plaintext", applicationValue: "my notes"},
    }
}

func storedColumnValue(t *testing.T, applicationValue string, deterministic bool) string {
    t.Helper()

    var stored driver.Value
    var valueErr error

    if true == deterministic {
        stored, valueErr = EncryptedDeterministicString(applicationValue).Value()
    } else {
        stored, valueErr = EncryptedString(applicationValue).Value()
    }

    if nil != valueErr {
        t.Fatalf("value: %v", valueErr)
    }

    return string(stored.([]byte))
}

func readColumnValue(t *testing.T, stored string, deterministic bool) string {
    t.Helper()

    if true == deterministic {
        var loaded EncryptedDeterministicString
        if scanErr := loaded.Scan([]byte(stored)); nil != scanErr {
            t.Fatalf("scan: %v", scanErr)
        }

        return string(loaded)
    }

    var loaded EncryptedString
    if scanErr := loaded.Scan([]byte(stored)); nil != scanErr {
        t.Fatalf("scan: %v", scanErr)
    }

    return string(loaded)
}

func newRotationCipher(t *testing.T) Cipher {
    t.Helper()

    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}}))
    installCipher(t, defaultCipherName, cipherInstance)

    return cipherInstance
}

func TestReencryptTransform_RotatesASealStoredAsDataInARandomColumnAsData(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    transform := (&Migrator{cipher: cipherInstance}).reencryptTransform(TableSpecification{Deterministic: false}, "v2")

    for _, testCase := range sealStoredAsDataCases(t, cipherInstance) {
        rotated, rotateErr := transform(storedColumnValue(t, testCase.applicationValue, false))
        if nil != rotateErr {
            t.Fatalf("%s: rotate: %v", testCase.name, rotateErr)
        }

        if keyId, _, _ := keyIdOf(rotated); "v2" != keyId {
            t.Fatalf("%s: expected the rotated value under v2, got %q", testCase.name, keyId)
        }

        if readBack := readColumnValue(t, rotated, false); testCase.applicationValue != readBack {
            t.Fatalf("%s: expected the rotated column to read back the value written, got %q", testCase.name, readBack)
        }
    }
}

func TestReencryptTransform_RotatesASealStoredAsDataInADeterministicColumnAsData(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    transform := (&Migrator{cipher: cipherInstance}).reencryptTransform(TableSpecification{Deterministic: true}, "v2")

    for _, testCase := range sealStoredAsDataCases(t, cipherInstance) {
        rotated, rotateErr := transform(storedColumnValue(t, testCase.applicationValue, true))
        if nil != rotateErr {
            t.Fatalf("%s: rotate: %v", testCase.name, rotateErr)
        }

        if keyId, _, _ := keyIdOf(rotated); "v2" != keyId {
            t.Fatalf("%s: expected the rotated value under v2, got %q", testCase.name, keyId)
        }

        if readBack := readColumnValue(t, rotated, true); testCase.applicationValue != readBack {
            t.Fatalf("%s: expected the rotated column to read back the value written, got %q", testCase.name, readBack)
        }

        if false == deterministicCandidateMatches(t, cipherInstance, testCase.applicationValue, rotated) {
            t.Fatalf("%s: expected the equality lookup to still find the rotated row", testCase.name)
        }
    }
}

func TestEncryptTransform_DeterministicReRunKeepsASealStoredAsDataAsData(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    transform := (&Migrator{cipher: cipherInstance}).encryptTransform(TableSpecification{Deterministic: true})

    for _, writtenDeterministic := range []bool{true, false} {
        for _, testCase := range sealStoredAsDataCases(t, cipherInstance) {
            stored := storedColumnValue(t, testCase.applicationValue, writtenDeterministic)

            converted, convertErr := transform(stored)
            if nil != convertErr {
                t.Fatalf("%s (written deterministic=%v): convert: %v", testCase.name, writtenDeterministic, convertErr)
            }

            if keyId, _, _ := keyIdOf(converted); "v1" != keyId {
                t.Fatalf("%s (written deterministic=%v): expected the value kept under v1, got %q", testCase.name, writtenDeterministic, keyId)
            }

            if readBack := readColumnValue(t, converted, true); testCase.applicationValue != readBack {
                t.Fatalf("%s (written deterministic=%v): expected the column to read back the value written, got %q", testCase.name, writtenDeterministic, readBack)
            }

            if false == deterministicCandidateMatches(t, cipherInstance, testCase.applicationValue, converted) {
                t.Fatalf("%s (written deterministic=%v): expected the equality lookup to find the converted row", testCase.name, writtenDeterministic)
            }
        }
    }
}

func TestMigrate_RefusesASealStoredAsDataThroughAForeignCipher(t *testing.T) {
    sealingCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}}))

    sealStoredAsData, sealErr := sealingCipher.Encrypt("another account's iban", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    migrator := &Migrator{cipher: NewFakeCipher()}

    transforms := map[string]func(string) (string, error){
        "rotation of a random column":        migrator.reencryptTransform(TableSpecification{Deterministic: false}, "v2"),
        "rotation of a deterministic column": migrator.reencryptTransform(TableSpecification{Deterministic: true}, "v2"),
        "deterministic encrypt re-run":       migrator.encryptTransform(TableSpecification{Deterministic: true}),
    }

    for name, transform := range transforms {
        refused, refusedErr := transform(sealStoredAsData)
        if nil == refusedErr {
            t.Fatalf("%s: expected a seal stored as data to be refused, got %q", name, refused)
        }

        if false == strings.Contains(refusedErr.Error(), "cannot re-seal as data") {
            t.Fatalf("%s: expected the foreign-cipher refusal, got: %v", name, refusedErr)
        }

        if true == strings.Contains(refusedErr.Error(), "another account's iban") {
            t.Fatalf("%s: expected the refusal to carry no plaintext, got: %v", name, refusedErr)
        }

        ordinary, ordinaryErr := transform("my notes")
        if nil != ordinaryErr || "my notes" != ordinary {
            t.Fatalf("%s: expected an ordinary value through the public doors, got %q (%v)", name, ordinary, ordinaryErr)
        }
    }
}

func TestMigrate_RefusesAMarkerFamilyPlaintextThroughAWrappedCipher(t *testing.T) {
    packageCipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2)}}))

    stored, sealErr := packageCipher.Encrypt("<ENC>\x00gcm9\x00v1:zzz", EncryptOptions{})
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    migrator := &Migrator{cipher: wrappedCipher{Cipher: packageCipher}}

    for name, transform := range map[string]func(string) (string, error){
        "rotation of a random column":        migrator.reencryptTransform(TableSpecification{Deterministic: false}, "v2"),
        "rotation of a deterministic column": migrator.reencryptTransform(TableSpecification{Deterministic: true}, "v2"),
        "deterministic encrypt re-run":       migrator.encryptTransform(TableSpecification{Deterministic: true}),
    } {
        refused, refusedErr := transform(stored)
        if nil == refusedErr || false == strings.Contains(refusedErr.Error(), "cannot re-seal as data") {
            t.Fatalf("%s: expected the foreign-cipher refusal, got %q (%v)", name, refused, refusedErr)
        }
    }
}

func TestMigrateReencrypt_RotatesASealStoredAsDataEndToEnd(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, nil)
    cipherInstance := newRotationCipher(t)
    migrator.cipher = cipherInstance

    cases := sealStoredAsDataCases(t, cipherInstance)

    rows := make([][]driver.Value, 0, len(cases))
    for index, testCase := range cases {
        rows = append(rows, []driver.Value{strconv.Itoa(index + 1), storedColumnValue(t, testCase.applicationValue, false)})
    }

    stub.responses = []scriptedSqlResponse{
        {fragment: "CHARACTER_SET_NAME", columns: []string{"COLUMN_NAME", "CHARACTER_SET_NAME"}},
        {fragment: "NOT LIKE", columns: []string{"longest"}, rows: [][]driver.Value{{nil}}},
        {fragment: "SUBSTRING_INDEX", columns: []string{"longest"}, rows: [][]driver.Value{{int64(100)}}},
        {
            fragment: "information_schema.COLUMNS",
            columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
            rows:     [][]driver.Value{{"varchar", int64(4096), int64(16384)}},
        },
        {fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: rows},
    }

    processed, runErr := migrator.MigrateReencrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}, "v2")
    if nil != runErr {
        t.Fatalf("rotate: %v", runErr)
    }

    if len(cases) != processed {
        t.Fatalf("expected %d rotated rows, got %d", len(cases), processed)
    }

    written := make([]string, 0, len(cases))
    for _, recorded := range stub.recordedArguments() {
        if true == strings.HasPrefix(recorded.query, "UPDATE ") {
            written = append(written, recorded.arguments[0].(string))
        }
    }

    if len(cases) != len(written) {
        t.Fatalf("expected %d updates, recorded %d", len(cases), len(written))
    }

    for index, testCase := range cases {
        if keyId, _, _ := keyIdOf(written[index]); "v2" != keyId {
            t.Fatalf("%s: expected the update to write a value under v2, got %q", testCase.name, keyId)
        }

        if readBack := readColumnValue(t, written[index], false); testCase.applicationValue != readBack {
            t.Fatalf("%s: expected the rotated row to read back the value written, got %q", testCase.name, readBack)
        }
    }
}

func TestMigrator_StopsOnAnUnknownSealFormatInEveryMode(t *testing.T) {
    for door, run := range map[string]func(migrator *Migrator, specification TableSpecification) (int, error){
        "encrypt": func(migrator *Migrator, specification TableSpecification) (int, error) {
            return migrator.MigrateEncrypt(testRuntime(), specification)
        },
        "reencrypt": func(migrator *Migrator, specification TableSpecification) (int, error) {
            return migrator.MigrateReencrypt(testRuntime(), specification, "v2")
        },
        "decrypt": func(migrator *Migrator, specification TableSpecification) (int, error) {
            return migrator.MigrateDecrypt(testRuntime(), specification)
        },
    } {
        t.Run(door, func(t *testing.T) {
            migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
                {
                    fragment: "information_schema.COLUMNS",
                    columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
                    rows:     [][]driver.Value{{"text", int64(65535), int64(65535)}},
                },
                {fragment: "MAX(LENGTH(", columns: []string{"longest"}, rows: [][]driver.Value{{nil}}},
                {
                    fragment: "ORDER BY",
                    columns:  []string{"id", "notes"},
                    rows:     [][]driver.Value{{"7", "<ENC>\x00gcm9\x00v1:zzz"}},
                },
            })
            migrator.cipher = newRotationCipher(t)

            _, runErr := run(migrator, TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
            if nil == runErr || false == exception.ChainIs(runErr, ErrSealFormatUnknown) {
                t.Fatalf("expected the run stopped with ErrSealFormatUnknown, got %v", runErr)
            }

            if "7" != exception.LogContext(runErr, exception.LogContextOptions{})["id"] {
                t.Fatalf("expected the refusal to name the row, got %v", exception.LogContext(runErr, exception.LogContextOptions{}))
            }

            for _, recorded := range stub.recorded() {
                if true == strings.HasPrefix(recorded, "UPDATE ") {
                    t.Fatalf("expected no update, saw: %s", recorded)
                }
            }
        })
    }
}

func TestMigrator_StopsOnABoundSealInAColumnTheSpecDoesNotBind(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    bound, _ := cipherInstance.(*aes256Cipher).sealBoundValue("my notes", sealBinding{label: "accounts.notes"}, false)

    for door, run := range migrateModeRuns {
        t.Run(door, func(t *testing.T) {
            migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
                {
                    fragment: "information_schema.COLUMNS",
                    columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
                    rows:     [][]driver.Value{{"text", int64(65535), int64(65535)}},
                },
                {fragment: "MAX(LENGTH(", columns: []string{"longest"}, rows: [][]driver.Value{{nil}}},
                {fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: [][]driver.Value{{"7", bound}}},
            })
            migrator.cipher = cipherInstance

            _, runErr := run(migrator, TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
            if nil == runErr || false == exception.ChainIs(runErr, ErrSealBound) {
                t.Fatalf("expected the run stopped with ErrSealBound, got %v", runErr)
            }

            if chain := strings.Join(exception.BuildCauseChain(runErr, 8), ": "); false == strings.Contains(chain, "TableSpecification.SealBindings") {
                t.Fatalf("expected the refusal to name TableSpecification.SealBindings, got %q", chain)
            }

            for _, recorded := range stub.recorded() {
                if true == strings.HasPrefix(recorded, "UPDATE ") {
                    t.Fatalf("expected no update, saw: %s", recorded)
                }
            }
        })
    }
}

func TestMigrator_TheWidthProbeLeavesOutTheMarkerFamily(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
        {
            fragment: "information_schema.COLUMNS",
            columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
            rows:     [][]driver.Value{{"varchar", int64(255), int64(1020)}},
        },
        {fragment: "MAX(LENGTH(", columns: []string{"longest"}, rows: [][]driver.Value{{nil}}},
        {fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: [][]driver.Value{{"7", "<ENC>\x00gcm9\x00v1:zzz"}}},
    })
    migrator.cipher = newRotationCipher(t)

    _, runErr := migrator.MigrateEncrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
    if nil == runErr || false == exception.ChainIs(runErr, ErrSealFormatUnknown) {
        t.Fatalf("expected the run stopped at the row with ErrSealFormatUnknown, got %v", runErr)
    }

    probed := false
    for _, recorded := range stub.recordedArguments() {
        if true == strings.Contains(recorded.query, "NOT LIKE") {
            probed = true
            if "<ENC>\x00%" != recorded.arguments[0] {
                t.Fatalf("expected the width probe to leave out the marker family, got %q", recorded.arguments[0])
            }
        }
    }

    if false == probed {
        t.Fatalf("expected the width probe to run")
    }
}

func TestMigrateDecrypt_StopsOnAnUnknownSealFormatThroughAPassThroughCipher(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
        {fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: [][]driver.Value{{"7", "<ENC>\x00gcm9\x00v1:zzz"}}},
    })
    migrator.cipher = NewFakeCipher()

    _, runErr := migrator.MigrateDecrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
    if nil == runErr || false == exception.ChainIs(runErr, ErrSealFormatUnknown) {
        t.Fatalf("expected ErrSealFormatUnknown, got %v", runErr)
    }

    for _, recorded := range stub.recorded() {
        if true == strings.HasPrefix(recorded, "UPDATE ") {
            t.Fatalf("expected no update, saw: %s", recorded)
        }
    }
}

func TestMigrateDecrypt_StopsOnAPlaintextOfTheMarkerFamily(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, nil)
    migrator.cipher = newRotationCipher(t)

    stub.responses = []scriptedSqlResponse{
        {fragment: "CHARACTER_SET_NAME", columns: []string{"COLUMN_NAME", "CHARACTER_SET_NAME"}},
        {
            fragment: "ORDER BY",
            columns:  []string{"id", "notes"},
            rows:     [][]driver.Value{{"4", storedColumnValue(t, "<ENC>\x00gcm9\x00v1:zzz", false)}},
        },
    }

    _, runErr := migrator.MigrateDecrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
    if nil == runErr || false == strings.Contains(fmt.Sprint(errors.Unwrap(runErr)), "itself an encrypted value") {
        t.Fatalf("expected the run stopped on the marker-family plaintext, got %v", runErr)
    }

    for _, recorded := range stub.recorded() {
        if true == strings.HasPrefix(recorded, "UPDATE ") {
            t.Fatalf("expected no update, saw: %s", recorded)
        }
    }
}

func TestMigrateDecrypt_StopsOnARowWhoseDecryptedValueIsASeal(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, nil)
    cipherInstance := newRotationCipher(t)
    migrator.cipher = cipherInstance

    cases := sealStoredAsDataCases(t, cipherInstance)

    stub.responses = []scriptedSqlResponse{
        {fragment: "CHARACTER_SET_NAME", columns: []string{"COLUMN_NAME", "CHARACTER_SET_NAME"}},
        {
            fragment: "ORDER BY",
            columns:  []string{"id", "notes"},
            rows: [][]driver.Value{
                {"1", storedColumnValue(t, "my notes", false)},
                {"2", storedColumnValue(t, cases[0].applicationValue, false)},
                {"3", storedColumnValue(t, "more notes", false)},
            },
        },
    }

    processed, runErr := migrator.MigrateDecrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
    if nil == runErr {
        t.Fatalf("expected the decrypt run to stop on the row whose decrypted value is a seal, processed=%d", processed)
    }

    refusal := errors.Unwrap(runErr)
    if nil == refusal || false == strings.Contains(refusal.Error(), "itself an encrypted value") {
        t.Fatalf("expected the refusal to say why, got: %v (cause %v)", runErr, refusal)
    }

    if true == strings.Contains(fmt.Sprint(exception.LogContext(runErr, exception.LogContextOptions{})), "another account's iban") || true == strings.Contains(fmt.Sprint(exception.LogContext(runErr, exception.LogContextOptions{})), cases[0].applicationValue) {
        t.Fatalf("expected the refusal to carry no value, got: %v", exception.LogContext(runErr, exception.LogContextOptions{}))
    }

    contextProvider, carriesContext := runErr.(exceptioncontract.ContextProvider)
    if false == carriesContext || "2" != contextProvider.Context()["id"] {
        t.Fatalf("expected the refusal to name the row, got %T %v", runErr, runErr)
    }

    if 1 != processed {
        t.Fatalf("expected the run to stop after the first row, processed=%d", processed)
    }

    written := make([]string, 0)
    for _, recorded := range stub.recordedArguments() {
        if true == strings.HasPrefix(recorded.query, "UPDATE ") {
            written = append(written, recorded.arguments[0].(string))
        }
    }

    if 1 != len(written) || "my notes" != written[0] {
        t.Fatalf("expected only the ordinary row decrypted and the refused row left sealed, wrote %q", written)
    }
}

func TestMigrateEncrypt_ConvertsOnlyAColumnOfAnotherCharsetInItsWidthAndGuard(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
        {fragment: "CHARACTER_SET_NAME", columns: []string{"COLUMN_NAME", "CHARACTER_SET_NAME"}, rows: [][]driver.Value{{"notes", "latin1"}, {"code", "utf8mb4"}, {"digest", nil}}},
        {fragment: "NOT LIKE", columns: []string{"longest"}, rows: [][]driver.Value{{nil}}},
        {fragment: "ORDER BY", columns: []string{"id", "notes", "code", "digest"}, rows: [][]driver.Value{{"1", "café", "c", "d"}}},
    })

    if _, runErr := migrator.MigrateEncrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes", "code", "digest"}}); nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    var widthProbes []string
    var update string
    for _, query := range stub.recorded() {
        if true == strings.Contains(query, "NOT LIKE") {
            widthProbes = append(widthProbes, query)
        }

        if true == strings.HasPrefix(query, "UPDATE") {
            update = query
        }
    }

    if 3 != len(widthProbes) || false == strings.Contains(widthProbes[0], "LENGTH(CONVERT(`notes` USING utf8mb4))") || true == strings.Contains(widthProbes[1], "CONVERT") || true == strings.Contains(widthProbes[2], "CONVERT") {
        t.Fatalf("expected the width of the latin1 column alone measured converted: %q", widthProbes)
    }

    if false == strings.Contains(update, "CAST(CONVERT(`notes` USING utf8mb4) AS BINARY) = ?") || false == strings.Contains(update, "CAST(`code` AS BINARY) = ?") || false == strings.Contains(update, "CAST(`digest` AS BINARY) = ?") {
        t.Fatalf("expected the latin1 column alone guarded converted: %q", update)
    }
}

func TestEncryptMigrate_SealsALatin1ColumnAndMeasuresItInConnectionBytes(t *testing.T) {
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
    cipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newRampKey()}}))
    migrator := NewMigrator(database, cipher)

    for _, charset := range []string{"latin1", "utf8mb4"} {
        table := "migrate_charset_" + charset

        database.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)
        defer database.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)

        createSql := "CREATE TABLE " + table + " (id BIGINT NOT NULL PRIMARY KEY, secret VARCHAR(255) CHARACTER SET " + charset + " NOT NULL)"
        if _, createErr := database.ExecContext(ctx, createSql); nil != createErr {
            t.Fatalf("create %s: %v", table, createErr)
        }

        if _, insertErr := database.ExecContext(ctx, "INSERT INTO "+table+" (id, secret) VALUES (1, ?)", "café"); nil != insertErr {
            t.Fatalf("insert %s: %v", table, insertErr)
        }

        charsetByColumn, charsetErr := migrator.columnCharsets(ctx, table)
        if nil != charsetErr {
            t.Fatalf("charsets %s: %v", table, charsetErr)
        }

        longest, _, longestErr := migrator.longestUnsealedLength(ctx, table, "secret", charsetByColumn["secret"])
        if nil != longestErr || 5 != longest {
            t.Fatalf("%s: expected the five utf8mb4 bytes of the value measured, got %d (%v)", charset, longest, longestErr)
        }

        sealed, runErr := migrator.MigrateEncrypt(testRuntimeUnder(ctx), TableSpecification{Table: table, PrimaryKey: "id", Columns: []string{"secret"}})
        if nil != runErr || 1 != sealed {
            t.Fatalf("%s: expected the row sealed, got %d (%v)", charset, sealed, runErr)
        }

        var stored string
        if scanErr := sqlDb.QueryRowContext(ctx, "SELECT secret FROM "+table+" WHERE id = 1").Scan(&stored); nil != scanErr {
            t.Fatalf("%s: read back: %v", charset, scanErr)
        }

        if plaintext, decryptErr := cipher.Decrypt(stored); nil != decryptErr || "café" != plaintext {
            t.Fatalf("%s: expected the stored seal to decrypt to the value, got %q (%v)", charset, plaintext, decryptErr)
        }
    }
}

func TestMigrator_RefusesABatchSizeOutsideItsRuleBeforeAnyQuery(t *testing.T) {
    for name, batchSize := range map[string]bound.Count{
        "zero":      bound.Of(0),
        "negative":  bound.Of(-1),
        "unlimited": bound.UnlimitedCount(),
    } {
        t.Run(name, func(t *testing.T) {
            migrator, scriptedDriver := newScriptedMigrator(t, nil)
            dialectQueries := len(scriptedDriver.recorded())
            specification := TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"iban"}, BatchSize: batchSize}

            for door, run := range map[string]func() (int, error){
                "encrypt":   func() (int, error) { return migrator.MigrateEncrypt(testRuntime(), specification) },
                "reencrypt": func() (int, error) { return migrator.MigrateReencrypt(testRuntime(), specification, "v2") },
                "decrypt":   func() (int, error) { return migrator.MigrateDecrypt(testRuntime(), specification) },
            } {
                _, runErr := run()
                if nil == runErr || "TableSpecification.BatchSize" != exception.LogContext(runErr, exception.LogContextOptions{})["field"] {
                    t.Fatalf("%s: expected the batch size refused naming the field, got %v", door, runErr)
                }
            }

            if recorded := scriptedDriver.recorded(); dialectQueries != len(recorded) {
                t.Fatalf("expected no query of the run before the refusal, got %v", recorded[dialectQueries:])
            }
        })
    }
}

func TestMigrator_AnAbsentBatchSizePagesByTheDefault(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
        {
            fragment:    "ORDER BY",
            columns:     []string{"id", "iban"},
            columnTypes: []string{"BIGINT", "VARCHAR"},
            rows:        nil,
        },
    })

    if _, runErr := migrator.MigrateDecrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"iban"}}); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    for _, recorded := range stub.recordedArguments() {
        if true == strings.Contains(recorded.query, "LIMIT") {
            if 1 != len(recorded.arguments) || int64(500) != recorded.arguments[0] {
                t.Fatalf("expected the page limited to 500 rows, got %v", recorded.arguments)
            }

            return
        }
    }

    t.Fatalf("expected the page query to have been recorded, got %+v", stub.recordedArguments())
}

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

func TestClassifyRunError_ReturnsOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if nil == (&Migrator{}).classifyRunError(TableSpecification{Table: "account"}, 0, "failed", newCyclicChainError()) {
            t.Error("expected the failure")
        }
    })
}

func assertBoundSeal(t *testing.T, cipherInstance Cipher, written string, plaintext string, keyId string, deterministic bool) {
    t.Helper()

    if false == strings.HasPrefix(written, boundMarkerPrefix) {
        t.Fatalf("expected a gcm2 seal, got %q", written)
    }

    opened, openErr := cipherInstance.(*aes256Cipher).openStored(written, accountsNotesSealBinding)
    if nil != openErr {
        t.Fatalf("expected the seal to open under accounts.notes: %v", openErr)
    }

    if plaintext != opened.plaintext || keyId != opened.keyId || deterministic != opened.deterministic {
        t.Fatalf("expected %q under %s deterministic=%v, got %q under %s deterministic=%v", plaintext, keyId, deterministic, opened.plaintext, opened.keyId, opened.deterministic)
    }
}

func assertNoUpdate(t *testing.T, stub *scriptedSqlDriver) {
    t.Helper()

    for _, recorded := range stub.recorded() {
        if true == strings.HasPrefix(recorded, "UPDATE ") {
            t.Fatalf("expected no update, saw: %s", recorded)
        }
    }
}

func TestMigrator_EncryptSealsABoundColumnUnderItsBinding(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", "my notes"}}))
    migrator.cipher = cipherInstance

    if _, runErr := migrator.MigrateEncrypt(testRuntime(), boundNotesSpec(false, false)); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    assertBoundSeal(t, cipherInstance, updatedValues(stub)["1"], "my notes", "v1", false)
}

func TestMigrator_EncryptRebindsAGcm1SealKeepingItsKeyAndMode(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    randomUnderV2, _ := cipherInstance.Encrypt("random v2", EncryptOptions{KeyId: "v2"})
    deterministicUnderV1, _ := cipherInstance.EncryptDeterministic("deterministic v1", EncryptOptions{KeyId: "v1"})
    deterministicUnderV2, _ := cipherInstance.EncryptDeterministic("deterministic v2", EncryptOptions{KeyId: "v2"})

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{
        {"1", randomUnderV2},
        {"2", deterministicUnderV1},
        {"3", deterministicUnderV2},
    }))
    migrator.cipher = cipherInstance

    if _, runErr := migrator.MigrateEncrypt(testRuntime(), boundNotesSpec(false, false)); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    written := updatedValues(stub)
    assertBoundSeal(t, cipherInstance, written["1"], "random v2", "v2", false)
    assertBoundSeal(t, cipherInstance, written["2"], "deterministic v1", "v1", true)
    assertBoundSeal(t, cipherInstance, written["3"], "deterministic v2", "v2", true)
}

func TestMigrator_EncryptLeavesABoundSealUntouched(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    randomUnderV2, _ := cipherInstance.(*aes256Cipher).sealBoundValueWithKeyId("random", "v2", accountsNotesSealBinding, false)
    deterministicUnderV1, _ := cipherInstance.(*aes256Cipher).sealBoundValueWithKeyId("deterministic", "v1", accountsNotesSealBinding, true)

    for name, testCase := range map[string]struct {
        deterministic bool
        rows          [][]driver.Value
    }{
        "random column":        {deterministic: false, rows: [][]driver.Value{{"1", randomUnderV2}, {"2", deterministicUnderV1}}},
        "deterministic column": {deterministic: true, rows: [][]driver.Value{{"2", deterministicUnderV1}}},
    } {
        t.Run(name, func(t *testing.T) {
            migrator, stub := newScriptedMigrator(t, scriptedNotesTable(testCase.rows))
            migrator.cipher = cipherInstance

            if _, runErr := migrator.MigrateEncrypt(testRuntime(), boundNotesSpec(false, testCase.deterministic)); nil != runErr {
                t.Fatalf("unexpected run error: %v", runErr)
            }

            assertNoUpdate(t, stub)
        })
    }
}

func TestMigrator_EncryptDeterministicConvertsARandomBoundSeal(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    randomUnderV2, _ := cipherInstance.(*aes256Cipher).sealBoundValueWithKeyId("random", "v2", accountsNotesSealBinding, false)

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", randomUnderV2}}))
    migrator.cipher = cipherInstance

    if _, runErr := migrator.MigrateEncrypt(testRuntime(), boundNotesSpec(false, true)); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    assertBoundSeal(t, cipherInstance, updatedValues(stub)["1"], "random", "v2", true)
}

func TestMigrator_EncryptOverAnUnboundColumnLeavesAGcm1SealUntouched(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    sealed, _ := cipherInstance.Encrypt("my notes", EncryptOptions{KeyId: "v2"})

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", sealed}}))
    migrator.cipher = cipherInstance

    if _, runErr := migrator.MigrateEncrypt(testRuntime(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    assertNoUpdate(t, stub)
}

func TestMigrator_ReencryptWritesABoundColumnUnderItsBinding(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    unboundRandom, _ := cipherInstance.Encrypt("unbound random", EncryptOptions{KeyId: "v1"})
    boundDeterministic, _ := cipherInstance.(*aes256Cipher).sealBoundValueWithKeyId("bound deterministic", "v1", accountsNotesSealBinding, true)

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{
        {"1", unboundRandom},
        {"2", boundDeterministic},
        {"3", "plain"},
    }))
    migrator.cipher = cipherInstance

    if _, runErr := migrator.MigrateReencrypt(testRuntime(), boundNotesSpec(false, false), "v2"); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    written := updatedValues(stub)
    assertBoundSeal(t, cipherInstance, written["1"], "unbound random", "v2", false)
    assertBoundSeal(t, cipherInstance, written["2"], "bound deterministic", "v2", true)
    assertBoundSeal(t, cipherInstance, written["3"], "plain", "v2", false)
}

func TestMigrator_ReencryptOntoTheCarriedKeyKeepsTheModeOfAGcm1Seal(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    unboundDeterministic, _ := cipherInstance.EncryptDeterministic("deterministic", EncryptOptions{KeyId: "v2"})
    boundRandom, _ := cipherInstance.(*aes256Cipher).sealBoundValueWithKeyId("random", "v2", accountsNotesSealBinding, false)
    boundDeterministic, _ := cipherInstance.(*aes256Cipher).sealBoundValueWithKeyId("bound deterministic", "v2", accountsNotesSealBinding, true)

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{
        {"1", unboundDeterministic},
        {"2", boundRandom},
        {"3", boundDeterministic},
    }))
    migrator.cipher = cipherInstance

    if _, runErr := migrator.MigrateReencrypt(testRuntime(), boundNotesSpec(false, false), "v2"); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    written := updatedValues(stub)
    assertBoundSeal(t, cipherInstance, written["1"], "deterministic", "v2", true)

    if _, rewritten := written["2"]; true == rewritten {
        t.Fatalf("expected the bound random seal under the target key left as it is, got %q", written["2"])
    }

    assertBoundSeal(t, cipherInstance, written["3"], "bound deterministic", "v2", false)
}

func TestMigrator_DecryptOpensABoundColumn(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    bound, _ := cipherInstance.(*aes256Cipher).sealBoundValue("bound", accountsNotesSealBinding, false)
    unbound, _ := cipherInstance.Encrypt("unbound", EncryptOptions{})

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", bound}, {"2", unbound}}))
    migrator.cipher = cipherInstance

    if _, runErr := migrator.MigrateDecrypt(testRuntime(), boundNotesSpec(false, false)); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    written := updatedValues(stub)
    if "bound" != written["1"] || "unbound" != written["2"] {
        t.Fatalf("expected the bare values written, got %v", written)
    }
}

func TestMigrator_StopsOnASealOfAnotherBinding(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    foreign, _ := cipherInstance.(*aes256Cipher).sealBoundValue("leads", sealBinding{label: "leads.notes"}, false)

    for door, run := range migrateModeRuns {
        t.Run(door, func(t *testing.T) {
            migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"7", foreign}}))
            migrator.cipher = cipherInstance

            _, runErr := run(migrator, boundNotesSpec(false, false))
            if nil == runErr {
                t.Fatal("expected the run stopped on another binding's seal")
            }

            if "7" != exception.LogContext(runErr, exception.LogContextOptions{})["id"] {
                t.Fatalf("expected the refusal to name the row, got %v", exception.LogContext(runErr, exception.LogContextOptions{}))
            }

            assertNoUpdate(t, stub)
        })
    }
}

func TestMigrator_RefusingUnboundStopsOnAGcm1SealAndOnPlaintext(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    unbound, _ := cipherInstance.Encrypt("unbound", EncryptOptions{})

    for _, door := range []string{"encrypt", "reencrypt"} {
        run := migrateModeRuns[door]
        for stored, value := range map[string]string{"gcm1": unbound, "plaintext": "plain"} {
            t.Run(door+"/"+stored, func(t *testing.T) {
                migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"7", value}}))
                migrator.cipher = cipherInstance

                _, runErr := run(migrator, boundNotesSpec(true, false))
                if nil == runErr || false == exception.ChainIs(runErr, ErrUnboundValueRefused) {
                    t.Fatalf("expected ErrUnboundValueRefused, got %v", runErr)
                }

                assertNoUpdate(t, stub)
            })
        }
    }
}

func TestMigrator_ASealStoredAsDataInABoundColumnStaysData(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    sealer := cipherInstance.(*aes256Cipher)
    seal, _ := cipherInstance.Encrypt("someone else's row", EncryptOptions{})
    boundData, _ := sealer.sealBoundValue(seal, accountsNotesSealBinding, false)
    unboundData, _ := sealer.sealValue(seal, false)

    for name, testCase := range map[string]struct {
        run           func(migrator *Migrator, specification TableSpecification) (int, error)
        stored        string
        specification TableSpecification
    }{
        "encrypt converting a bound seal": {run: migrateModeRuns["encrypt"], stored: boundData, specification: boundNotesSpec(false, true)},
        "encrypt rebinding a gcm1 seal":   {run: migrateModeRuns["encrypt"], stored: unboundData, specification: boundNotesSpec(false, false)},
        "reencrypt":                       {run: migrateModeRuns["reencrypt"], stored: boundData, specification: boundNotesSpec(false, false)},
    } {
        t.Run(name, func(t *testing.T) {
            migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", testCase.stored}}))
            migrator.cipher = cipherInstance

            if _, runErr := testCase.run(migrator, testCase.specification); nil != runErr {
                t.Fatalf("unexpected run error: %v", runErr)
            }

            written, updated := updatedValues(stub)["1"]
            if false == updated {
                t.Fatal("expected the row re-sealed")
            }

            opened, openErr := sealer.openStored(written, accountsNotesSealBinding)
            if nil != openErr || seal != opened.plaintext {
                t.Fatalf("expected the row to read back as the seal string, got %q, %v", opened.plaintext, openErr)
            }
        })
    }
}

func TestMigrator_RefusesASpecItCannotBind(t *testing.T) {
    cipherInstance := newRotationCipher(t)

    for name, testCase := range map[string]struct {
        cipher        Cipher
        specification TableSpecification
    }{
        "an unlisted column": {
            cipher:        cipherInstance,
            specification: TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}, SealBindings: map[string]ColumnSealBinding{"body": {Label: "accounts.body"}}},
        },
        "a label outside the grammar": {
            cipher:        cipherInstance,
            specification: TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}, SealBindings: map[string]ColumnSealBinding{"notes": {Label: "notes"}}},
        },
        "a wrapped cipher": {
            cipher:        wrappedCipher{Cipher: cipherInstance},
            specification: boundNotesSpec(false, false),
        },
    } {
        for door, run := range migrateModeRuns {
            t.Run(name+"/"+door, func(t *testing.T) {
                migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", "plain"}}))
                migrator.cipher = testCase.cipher

                _, runErr := run(migrator, testCase.specification)
                if nil == runErr || "TableSpecification.SealBindings" != exception.LogContext(runErr, exception.LogContextOptions{})["field"] {
                    t.Fatalf("expected a refusal naming TableSpecification.SealBindings, got %v", runErr)
                }

                if queries := runQueries(stub); 0 != len(queries) {
                    t.Fatalf("expected no query before the refusal, saw %v", queries)
                }
            })
        }
    }
}

func TestMigrator_CapacityCountsABoundSealAsSealed(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    migrator, stub := newScriptedMigrator(t, scriptedNotesTable(nil))
    migrator.cipher = cipherInstance

    if _, runErr := migrator.MigrateReencrypt(testRuntime(), boundNotesSpec(false, false), "v2"); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    probed := false
    for _, recorded := range stub.recordedArguments() {
        if false == strings.Contains(recorded.query, "SUBSTRING_INDEX") {
            continue
        }

        probed = true
        arguments := fmt.Sprint(recorded.arguments)
        if false == strings.Contains(arguments, markerPrefix+"%") || false == strings.Contains(arguments, boundMarkerPrefix+"%") {
            t.Fatalf("expected the probe to name both prefixes, got %q", arguments)
        }
    }

    if false == probed {
        t.Fatal("expected the sealed width probe to run")
    }
}

func TestMigrator_CountsWhatItBoundAndWhatItSealed(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    unbound, _ := cipherInstance.Encrypt("unbound", EncryptOptions{})
    bound, _ := cipherInstance.(*aes256Cipher).sealBoundValue("bound", accountsNotesSealBinding, false)

    migrator, _ := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", "plain"}, {"2", unbound}, {"3", bound}, {"4", "other plain"}}))
    migrator.cipher = cipherInstance

    stats, runErr := migrator.migrateEncrypt(context.Background(), boundNotesSpec(false, false))
    if nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if 4 != stats.rows || 1 != stats.boundFromUnbound || 2 != stats.sealedFromPlaintext {
        t.Fatalf("expected 4 rows, 1 bound from unbound, 2 sealed from plaintext, got %+v", stats)
    }
}

func TestMigrator_CountsNoBindingOnAnUnboundColumn(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    random, _ := cipherInstance.Encrypt("random", EncryptOptions{})

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", random}}))
    migrator.cipher = cipherInstance

    stats, runErr := migrator.migrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}, Deterministic: true})
    if nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if _, rewritten := updatedValues(stub)["1"]; false == rewritten {
        t.Fatal("expected the random seal converted")
    }

    if 1 != stats.rows || 0 != stats.boundFromUnbound || 0 != stats.sealedFromPlaintext {
        t.Fatalf("expected one row and nothing bound or sealed, got %+v", stats)
    }
}

func TestMigrator_DecryptStopsOnABoundSealOfASeal(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    seal, _ := cipherInstance.Encrypt("someone else's row", EncryptOptions{})
    boundData, _ := cipherInstance.(*aes256Cipher).sealBoundValue(seal, accountsNotesSealBinding, false)

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"7", boundData}}))
    migrator.cipher = cipherInstance

    _, runErr := migrator.MigrateDecrypt(testRuntime(), boundNotesSpec(false, false))
    if nil == runErr || "7" != exception.LogContext(runErr, exception.LogContextOptions{})["id"] {
        t.Fatalf("expected the run stopped at the row, got %v", runErr)
    }

    assertNoUpdate(t, stub)
}

func TestMigrator_ReencryptDeterministicConvertsABoundColumn(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    unboundRandom, _ := cipherInstance.Encrypt("unbound random", EncryptOptions{KeyId: "v1"})
    boundRandom, _ := cipherInstance.(*aes256Cipher).sealBoundValueWithKeyId("bound random", "v2", accountsNotesSealBinding, false)

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", unboundRandom}, {"2", boundRandom}}))
    migrator.cipher = cipherInstance

    if _, runErr := migrator.MigrateReencrypt(testRuntime(), boundNotesSpec(false, true), "v2"); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    written := updatedValues(stub)
    assertBoundSeal(t, cipherInstance, written["1"], "unbound random", "v2", true)
    assertBoundSeal(t, cipherInstance, written["2"], "bound random", "v2", true)
}

func TestMigrator_DecryptRefusesARefusingBindingBeforeAnyQuery(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    bound, _ := cipherInstance.(*aes256Cipher).sealBoundValue("bound", accountsNotesSealBinding, false)

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", bound}}))
    migrator.cipher = cipherInstance

    _, runErr := migrator.MigrateDecrypt(testRuntime(), boundNotesSpec(true, false))
    if nil == runErr || "TableSpecification.SealBindings" != exception.LogContext(runErr, exception.LogContextOptions{})["field"] {
        t.Fatalf("expected a refusal naming TableSpecification.SealBindings, got %v", runErr)
    }

    if queries := runQueries(stub); 0 != len(queries) {
        t.Fatalf("expected no query before the refusal, saw %v", queries)
    }
}

func TestMigrator_ASpecBindsItsBoundColumnAlone(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    migrator, stub := newScriptedMigrator(t, []scriptedSqlResponse{
        {
            fragment: "information_schema.COLUMNS",
            columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
            rows:     [][]driver.Value{{"text", int64(65535), int64(65535)}},
        },
        {fragment: "MAX(LENGTH(", columns: []string{"longest"}, rows: [][]driver.Value{{nil}}},
        {fragment: "ORDER BY", columns: []string{"id", "notes", "body"}, rows: [][]driver.Value{{"1", "plain notes", "plain body"}}},
    })
    migrator.cipher = cipherInstance

    specification := boundNotesSpec(false, false)
    specification.Columns = []string{"notes", "body"}

    if _, runErr := migrator.MigrateEncrypt(testRuntime(), specification); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    var update recordedScriptedQuery
    for _, recorded := range stub.recordedArguments() {
        if true == strings.HasPrefix(recorded.query, "UPDATE ") {
            update = recorded
        }
    }

    if 2 > len(update.arguments) {
        t.Fatalf("expected one update writing both columns, got %v", update)
    }

    assertBoundSeal(t, cipherInstance, fmt.Sprint(update.arguments[0]), "plain notes", "v1", false)

    if body := fmt.Sprint(update.arguments[1]); false == strings.HasPrefix(body, markerPrefix) {
        t.Fatalf("expected the unbound column sealed gcm1, got %q", body)
    }
}

func TestMigrator_ABoundSpecOverTheFakeCipherIsTheIdentity(t *testing.T) {
    for door, run := range migrateModeRuns {
        t.Run(door, func(t *testing.T) {
            migrator, stub := newScriptedMigrator(t, scriptedNotesTable([][]driver.Value{{"1", "plain"}}))
            migrator.cipher = NewFakeCipher()

            if _, runErr := run(migrator, boundNotesSpec(false, false)); nil != runErr {
                t.Fatalf("unexpected run error: %v", runErr)
            }

            assertNoUpdate(t, stub)
        })
    }
}

func TestMigrator_EveryDoorRefusesAnAbsentRuntime(t *testing.T) {
    migrator := NewMigrator(newMysqlDatabase(), NewFakeCipher())
    specification := TableSpecification{}

    _, encryptErr := migrator.MigrateEncrypt(nil, specification)
    _, reencryptErr := migrator.MigrateReencrypt(nil, specification, "next")
    _, decryptErr := migrator.MigrateDecrypt(nil, specification)
    capacityErr := migrator.EnsureColumnCapacity(nil, specification)
    reencryptCapacityErr := migrator.EnsureColumnCapacityForReencrypt(nil, specification, "next")

    for doorName, doorErr := range map[string]error{"MigrateEncrypt": encryptErr, "MigrateReencrypt": reencryptErr, "MigrateDecrypt": decryptErr, "EnsureColumnCapacity": capacityErr, "EnsureColumnCapacityForReencrypt": reencryptCapacityErr} {
        if nil == doorErr || "the runtime is nil or carries no context" != doorErr.Error() {
            t.Fatalf("%s: expected the absent runtime refused, got %v", doorName, doorErr)
        }
    }
}
