package encrypt

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "io"
    "slices"
    "strings"
    "sync"
    "sync/atomic"
    "testing"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

type scriptedSqlResponse struct {
    fragment    string
    columns     []string
    columnTypes []string
    rows        [][]driver.Value
    err         error
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
    execErr error
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

func (instance *scriptedSqlStatement) Exec(arguments []driver.Value) (driver.Result, error) {
    instance.shared.mutex.Lock()
    instance.shared.queryArguments = append(instance.shared.queryArguments, recordedScriptedQuery{
        query:     instance.query,
        arguments: append([]driver.Value(nil), arguments...),
    })
    execErr := instance.shared.execErr
    instance.shared.mutex.Unlock()

    if nil != execErr {
        return nil, execErr
    }

    return driver.RowsAffected(1), nil
}

func (instance *scriptedSqlStatement) Query(arguments []driver.Value) (driver.Rows, error) {
    instance.shared.mutex.Lock()
    responses := withSchemaJudgeDefaults(instance.shared.responses)
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

func withSchemaJudgeDefaults(responses []scriptedSqlResponse) []scriptedSqlResponse {
    answers := func(fragment string) bool {
        for _, response := range responses {
            if true == strings.Contains(response.fragment, fragment) {
                return true
            }
        }

        return false
    }

    columns := []string{"id"}
    for _, response := range responses {
        if "ORDER BY" == response.fragment && 0 < len(response.columns) {
            columns = response.columns
        }
    }

    for _, response := range responses {
        if "CHARACTER_SET_NAME" != response.fragment {
            continue
        }

        for _, row := range response.rows {
            if name, isName := row[0].(string); true == isName && false == slices.Contains(columns, name) {
                columns = append(columns, name)
            }
        }
    }

    defaults := make([]scriptedSqlResponse, 0, 3)
    if false == answers("SELECT COLUMN_NAME FROM") {
        rows := make([][]driver.Value, 0, len(columns))
        for _, column := range columns {
            rows = append(rows, []driver.Value{column})
        }

        defaults = append(defaults, scriptedSqlResponse{fragment: "SELECT COLUMN_NAME FROM", columns: []string{"COLUMN_NAME"}, rows: rows})
    }

    if false == answers("TABLE_TYPE") {
        defaults = append(defaults, scriptedSqlResponse{fragment: "TABLE_TYPE", columns: []string{"TABLE_TYPE"}, rows: [][]driver.Value{{"BASE TABLE"}}})
    }

    if false == answers("STATISTICS") {
        defaults = append(defaults, scriptedSqlResponse{fragment: "STATISTICS", columns: []string{"INDEX_NAME", "COLUMN_NAME", "NON_UNIQUE"}, rows: [][]driver.Value{{"PRIMARY", columns[0], int64(0)}}})
    }

    return append(defaults, responses...)
}

var scriptedSqlSequence atomic.Uint64

func newScriptedMigrator(t *testing.T, responses []scriptedSqlResponse) (*Migrator, *scriptedSqlDriver) {
    t.Helper()

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

/* distinct from newKey in every byte, which a rotation between the two keys needs */
func newRampKey() []byte {
    key := make([]byte, 32)
    for index := range key {
        key[index] = byte(index + 1)
    }
    return key
}

/* keeps the command's own error apart from a refused command line, which the dispatch answers */
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

/* carries none of the package's unexported doors, as an application's metrics wrapper does */
type wrappedCipher struct {
    Cipher
}
