package encrypt

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "io"
    "strings"
    "sync"
    "sync/atomic"
    "testing"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

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
    fragment string
    columns  []string
    rows     [][]driver.Value
    err      error
}

type scriptedSqlDriver struct {
    mutex     sync.Mutex
    responses []scriptedSqlResponse
    queries   []string
}

func (instance *scriptedSqlDriver) Open(name string) (driver.Conn, error) {
    return &scriptedSqlConnection{shared: instance}, nil
}

func (instance *scriptedSqlDriver) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string(nil), instance.queries...)
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

func (instance *scriptedSqlStatement) Exec(arguments []driver.Value) (driver.Result, error) {
    return driver.RowsAffected(1), nil
}

func (instance *scriptedSqlStatement) Query(arguments []driver.Value) (driver.Rows, error) {
    instance.shared.mutex.Lock()
    responses := instance.shared.responses
    instance.shared.mutex.Unlock()

    for _, response := range responses {
        if false == strings.Contains(instance.query, response.fragment) {
            continue
        }

        if nil != response.err {
            return nil, response.err
        }

        return &scriptedSqlRows{columns: response.columns, remaining: response.rows}, nil
    }

    return nil, errors.New("the scripted driver has no response for: " + instance.query)
}

type scriptedSqlRows struct {
    columns   []string
    remaining [][]driver.Value
}

func (instance *scriptedSqlRows) Columns() []string {
    return instance.columns
}

func (instance *scriptedSqlRows) Close() error {
    return nil
}

func (instance *scriptedSqlRows) Next(destination []driver.Value) error {
    if 0 == len(instance.remaining) {
        return io.EOF
    }

    copy(destination, instance.remaining[0])
    instance.remaining = instance.remaining[1:]

    return nil
}

var scriptedSqlSequence atomic.Uint64

func newScriptedMigrator(t *testing.T, responses []scriptedSqlResponse) (*Migrator, *scriptedSqlDriver) {
    t.Helper()

    stub := &scriptedSqlDriver{responses: responses}
    driverName := fmt.Sprintf("scripted-migrate-%d", scriptedSqlSequence.Add(1))
    sql.Register(driverName, stub)

    sqlDatabase, openErr := sql.Open(driverName, "scripted")
    if nil != openErr {
        t.Fatalf("open scripted database: %v", openErr)
    }

    t.Cleanup(func() { _ = sqlDatabase.Close() })

    provider := NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})

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
