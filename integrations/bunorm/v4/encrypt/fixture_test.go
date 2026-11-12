package encrypt

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "io"
    "reflect"
    "strings"
    "sync"
    "sync/atomic"
    "testing"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* installCipher restores the name's previous entry when the test ends, so no cipher leaks into the next test */
func installCipher(t *testing.T, name string, cipherInstance Cipher) {
    t.Helper()

    cipherRegistryMutex.RLock()
    previous, existed := cipherRegistry[name]
    cipherRegistryMutex.RUnlock()

    storeCipher(name, cipherInstance)

    t.Cleanup(func() {
        if true == existed {
            storeCipher(name, previous)
            return
        }

        uninstallCipher(name)
    })
}

/* the bound markers of the pins: two columns of the crm compartment, one of the default cipher, and one never declared */
type crmIbanBinding struct{}

func (instance crmIbanBinding) CipherName() string {
    return "test:crm"
}

func (instance crmIbanBinding) SealBinding() string {
    return "customer.iban"
}

type crmEmailBinding struct{}

func (instance crmEmailBinding) CipherName() string {
    return "test:crm"
}

func (instance crmEmailBinding) SealBinding() string {
    return "customer.email"
}

type defaultSecretBinding struct {
    DefaultCipherRef
}

func (instance defaultSecretBinding) SealBinding() string {
    return "users.secret"
}

type undeclaredBinding struct{}

func (instance undeclaredBinding) CipherName() string {
    return "test:crm"
}

func (instance undeclaredBinding) SealBinding() string {
    return "customer.undeclared"
}

/* installSealBinding declares a marker's binding for one test, the earlier entry restored after it */
func installSealBinding(t *testing.T, ref SealBindingRef, refuseUnbound bool) {
    t.Helper()

    marker := reflect.TypeOf(ref)
    previous, existed := sealBindingOf(marker)

    storeSealBinding(marker, sealBinding{label: ref.SealBinding(), refuseUnbound: refuseUnbound})

    t.Cleanup(func() {
        if true == existed {
            storeSealBinding(marker, previous)
            return
        }

        sealBindingRegistryMutex.Lock()
        defer sealBindingRegistryMutex.Unlock()

        delete(sealBindingRegistry, marker)
    })
}

/* newCrmCipher installs a package cipher of two keys, crm-v2 current, under the crm compartment for one test */
func newCrmCipher(t *testing.T) Cipher {
    t.Helper()

    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "crm-v2", Keys: map[string][]byte{"crm-v1": newKey(11), "crm-v2": newKey(12)}}))
    installCipher(t, crmCipherRef{}.CipherName(), cipherInstance)

    return cipherInstance
}

/* recoverRefusalContext runs a callback that must panic with a melody error and answers that error's context */
func recoverRefusalContext(t *testing.T, callback func()) map[string]any {
    t.Helper()

    var refusalContext map[string]any
    func() {
        defer func() {
            recovered := recover()
            refusalErr, isErr := recovered.(error)
            if false == isErr {
                t.Fatalf("expected a panic with an error, got %v", recovered)
            }

            refusalContext = exception.FromError(refusalErr).Context()
        }()

        callback()
    }()

    return refusalContext
}

func uninstallCipher(name string) {
    cipherRegistryMutex.Lock()
    defer cipherRegistryMutex.Unlock()

    delete(cipherRegistry, name)
}

func newKey(filler byte) []byte {
    key := make([]byte, 32)
    for index := range key {
        key[index] = filler
    }
    return key
}

func deterministicCandidateMatches(t *testing.T, cipher Cipher, plaintext string, encrypted string) bool {
    t.Helper()

    candidates, candidatesErr := cipher.CiphertextCandidates(plaintext)
    if nil != candidatesErr {
        t.Fatalf("candidates: %v", candidatesErr)
    }

    for _, candidate := range candidates {
        if true == bytes.Equal(candidate, []byte(encrypted)) {
            return true
        }
    }

    return false
}

/* scriptedSqlDriver answers each query by the first matching fragment, so a test can shape the information_schema answer, the keyset pages and the update results independently. */
type scriptedSqlResponse struct {
    fragment    string
    columns     []string
    columnTypes []string
    rows        [][]driver.Value
    err         error
    /* nextErr is what the rows answer once the scripted ones are read, in place of the end of the set — a failure surfacing through Rows.Err */
    nextErr error
}

type recordedScriptedQuery struct {
    query     string
    arguments []driver.Value
}

type scriptedSqlDriver struct {
    mutex          sync.Mutex
    responses      []scriptedSqlResponse
    queries        []string
    queryArguments []recordedScriptedQuery
}

func (instance *scriptedSqlDriver) Open(name string) (driver.Conn, error) {
    return &scriptedSqlConnection{shared: instance}, nil
}

func (instance *scriptedSqlDriver) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string(nil), instance.queries...)
}

func (instance *scriptedSqlDriver) recordedArguments() []recordedScriptedQuery {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]recordedScriptedQuery(nil), instance.queryArguments...)
}

type scriptedSqlConnection struct {
    shared *scriptedSqlDriver
}

func (instance *scriptedSqlConnection) Prepare(query string) (driver.Stmt, error) {
    instance.shared.mutex.Lock()
    instance.shared.queries = append(instance.shared.queries, query)
    instance.shared.mutex.Unlock()

    return &scriptedSqlStatement{shared: instance.shared, query: query}, nil
}

func (instance *scriptedSqlConnection) Close() error {
    return nil
}

func (instance *scriptedSqlConnection) Begin() (driver.Tx, error) {
    return nil, errors.New("transactions are not supported by the scripted driver")
}

type scriptedSqlStatement struct {
    shared *scriptedSqlDriver
    query  string
}

func (instance *scriptedSqlStatement) Close() error {
    return nil
}

func (instance *scriptedSqlStatement) NumInput() int {
    return -1
}

/* Exec records its arguments beside the queries', so a test can read the value an update wrote */
func (instance *scriptedSqlStatement) Exec(arguments []driver.Value) (driver.Result, error) {
    instance.shared.mutex.Lock()
    instance.shared.queryArguments = append(instance.shared.queryArguments, recordedScriptedQuery{
        query:     instance.query,
        arguments: append([]driver.Value(nil), arguments...),
    })
    instance.shared.mutex.Unlock()

    return driver.RowsAffected(1), nil
}

func (instance *scriptedSqlStatement) Query(arguments []driver.Value) (driver.Rows, error) {
    instance.shared.mutex.Lock()
    responses := instance.shared.responses
    instance.shared.queryArguments = append(instance.shared.queryArguments, recordedScriptedQuery{
        query:     instance.query,
        arguments: append([]driver.Value(nil), arguments...),
    })
    instance.shared.mutex.Unlock()

    for _, response := range responses {
        if false == strings.Contains(instance.query, response.fragment) {
            continue
        }

        if nil != response.err {
            return nil, response.err
        }

        return &scriptedSqlRows{columns: response.columns, columnTypes: response.columnTypes, remaining: response.rows, nextErr: response.nextErr}, nil
    }

    return nil, errors.New("the scripted driver has no response for: " + instance.query)
}

type scriptedSqlRows struct {
    columns     []string
    columnTypes []string
    remaining   [][]driver.Value
    nextErr     error
}

func (instance *scriptedSqlRows) Columns() []string {
    return instance.columns
}

/* ColumnTypeDatabaseTypeName reports the scripted type name, empty when the script declared none — the same silence a driver without the capability answers. */
func (instance *scriptedSqlRows) ColumnTypeDatabaseTypeName(index int) string {
    if index >= len(instance.columnTypes) {
        return ""
    }

    return instance.columnTypes[index]
}

func (instance *scriptedSqlRows) Close() error {
    return nil
}

func (instance *scriptedSqlRows) Next(destination []driver.Value) error {
    if 0 == len(instance.remaining) {
        if nil != instance.nextErr {
            return instance.nextErr
        }

        return io.EOF
    }

    copy(destination, instance.remaining[0])
    instance.remaining = instance.remaining[1:]

    return nil
}

var scriptedSqlSequence atomic.Uint64

func newScriptedMigrator(t *testing.T, responses []scriptedSqlResponse) (*Migrator, *scriptedSqlDriver) {
    t.Helper()

    /* every run reads the table's charsets first; a script that does not answer it reads every column as utf8mb4, the shape of the tables these pins describe */
    answersCharsets := false
    for _, response := range responses {
        if true == strings.Contains(response.fragment, "CHARACTER_SET_NAME") {
            answersCharsets = true
        }
    }

    if false == answersCharsets {
        responses = append([]scriptedSqlResponse{{fragment: "CHARACTER_SET_NAME", columns: []string{"COLUMN_NAME", "CHARACTER_SET_NAME"}}}, responses...)
    }

    stub := &scriptedSqlDriver{responses: responses}
    driverName := fmt.Sprintf("scripted-migrate-%d", scriptedSqlSequence.Add(1))
    sql.Register(driverName, stub)

    sqlDatabase, openErr := sql.Open(driverName, "scripted")
    if nil != openErr {
        t.Fatalf("open scripted database: %v", openErr)
    }

    t.Cleanup(func() { _ = sqlDatabase.Close() })

    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})

    return &Migrator{
        db:     bun.NewDB(sqlDatabase, mysqldialect.New()),
        cipher: NewCipher(provider),
    }, stub
}

type fakeRuntime struct{}

func (instance fakeRuntime) Context() context.Context {
    return context.Background()
}

func (instance fakeRuntime) Scope() containercontract.Scope {
    return nil
}

func (instance fakeRuntime) Container() containercontract.Container {
    return nil
}

/* newRampKey is the 32-byte ramp key the live mysql suites seal with; the sibling newKey fills every byte alike, and the two must stay distinguishable because a rotation test needs two keys that differ in every position. */
func newRampKey() []byte {
    key := make([]byte, 32)
    for index := range key {
        key[index] = byte(index + 1)
    }
    return key
}

/* capturingCommand delegates to the command under test and keeps its error, so a test can tell a refused command line — which the dispatch answers — from the command's own failure, which is what it is asserting. */
type capturingCommand struct {
    clicontract.Command
    runtimeInstance runtimecontract.Runtime
    capturedErr     error
}

var _ clicontract.Command = (*capturingCommand)(nil)

func (instance *capturingCommand) Run(
    dispatchedRuntime runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    instance.capturedErr = instance.Command.Run(instance.runtimeInstance, commandContext)

    return nil
}

/* wrappedCipher forwards every door to the package cipher it embeds, the shape of an application's metrics or tracing wrapper: it carries none of the package's unexported doors */
type wrappedCipher struct {
    Cipher
}
