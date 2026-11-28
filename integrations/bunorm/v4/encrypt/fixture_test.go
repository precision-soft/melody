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
    "slices"
    "strings"
    "sync"
    "sync/atomic"
    "testing"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

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

func storeSealBinding(marker reflect.Type, binding sealBinding) {
    sealBindingRegistryMutex.Lock()
    defer sealBindingRegistryMutex.Unlock()

    sealBindingRegistry[marker] = binding
}

func storeCipher(name string, cipherInstance Cipher) {
    if true == isNilCipher(cipherInstance) {
        exception.Panic(exception.NewError("cipher instance is nil", map[string]any{"cipherName": name}, nil))
    }

    cipherRegistryMutex.Lock()
    defer cipherRegistryMutex.Unlock()

    cipherRegistry[name] = cipherInstance
}

func preserveRegistries(t *testing.T) {
    t.Helper()

    cipherRegistryMutex.RLock()
    sealBindingRegistryMutex.RLock()
    ciphers := make(map[string]Cipher, len(cipherRegistry))
    for name, cipherInstance := range cipherRegistry {
        ciphers[name] = cipherInstance
    }
    bindings := make(map[reflect.Type]sealBinding, len(sealBindingRegistry))
    for marker, binding := range sealBindingRegistry {
        bindings[marker] = binding
    }
    sealBindingRegistryMutex.RUnlock()
    cipherRegistryMutex.RUnlock()

    t.Cleanup(func() {
        cipherRegistryMutex.Lock()
        defer cipherRegistryMutex.Unlock()

        sealBindingRegistryMutex.Lock()
        defer sealBindingRegistryMutex.Unlock()

        cipherRegistry = ciphers
        sealBindingRegistry = bindings
    })
}

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
    DefaultCipherReference
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

func installSealBinding(t *testing.T, reference SealBindingReference, refuseUnbound bool) {
    t.Helper()

    marker := reflect.TypeOf(reference)
    previous, existed := sealBindingOf(marker)

    storeSealBinding(marker, sealBinding{label: reference.SealBinding(), refuseUnbound: refuseUnbound})

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

func newCrmCipher(t *testing.T) Cipher {
    t.Helper()

    cipherInstance := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "crm-v2", Keys: map[string][]byte{"crm-v1": newKey(11), "crm-v2": newKey(12)}}))
    installCipher(t, crmCipherReference{}.CipherName(), cipherInstance)

    return cipherInstance
}

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
    execErr        error
    responses      []scriptedSqlResponse
    queries        []string
    connectionOf   []int
    opened         int
    failFirstPing  bool
    queryArguments []recordedScriptedQuery
}

func (instance *scriptedSqlDriver) Open(name string) (driver.Conn, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.opened++

    return &scriptedSqlConnection{shared: instance, number: instance.opened}, nil
}

func (instance *scriptedSqlDriver) connectionsOf(fragment string) map[int]bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    numbers := map[int]bool{}
    for index, query := range instance.queries {
        if true == strings.Contains(query, fragment) {
            numbers[instance.connectionOf[index]] = true
        }
    }

    return numbers
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
    number int
}

func (instance *scriptedSqlConnection) Prepare(query string) (driver.Stmt, error) {
    instance.shared.mutex.Lock()
    instance.shared.queries = append(instance.shared.queries, query)
    instance.shared.connectionOf = append(instance.shared.connectionOf, instance.number)
    instance.shared.mutex.Unlock()

    return &scriptedSqlStatement{shared: instance.shared, query: query}, nil
}

func (instance *scriptedSqlConnection) Ping(ctx context.Context) error {
    if true == instance.shared.failFirstPing && 1 == instance.number {
        return driver.ErrBadConn
    }

    return nil
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

    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1)}})

    return &Migrator{
        db:     bun.NewDB(sqlDatabase, mysqldialect.New()),
        cipher: NewCipher(provider),
    }, stub
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
    if false == answers("IS_NULLABLE") {
        rows := make([][]driver.Value, 0, len(columns))
        for _, column := range columns {
            rows = append(rows, []driver.Value{column, "NO"})
        }

        defaults = append(defaults, scriptedSqlResponse{fragment: "IS_NULLABLE", columns: []string{"COLUMN_NAME", "IS_NULLABLE"}, rows: rows})
    }

    if false == answers("STATISTICS") {
        defaults = append(defaults, scriptedSqlResponse{fragment: "STATISTICS", columns: []string{"INDEX_NAME", "COLUMN_NAME", "NON_UNIQUE"}, rows: [][]driver.Value{{"PRIMARY", columns[0], int64(0)}}})
    }

    if false == answers("sql_mode") {
        defaults = append(defaults, scriptedSqlResponse{fragment: "@@SESSION.sql_mode", columns: []string{"@@SESSION.sql_mode"}, rows: [][]driver.Value{{"STRICT_TRANS_TABLES"}}})
    }

    return append(defaults, responses...)
}

func testRuntime() containercontract.Runtime {
    return testRuntimeUnder(context.Background())
}

func testRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

type fakeRuntime struct{}

func (instance fakeRuntime) Context() context.Context {
    return context.Background()
}

func (instance fakeRuntime) Scope() containercontract.Scope {
    return nil
}

func (instance fakeRuntime) Kind() containercontract.Kind {
    return containercontract.KindRun
}

/* a ramp, distinguishable from newKey's repeated byte, since a rotation test needs two keys that differ */
func newRampKey() []byte {
    key := make([]byte, 32)
    for index := range key {
        key[index] = byte(index + 1)
    }
    return key
}

type capturingCommand struct {
    clicontract.Command
    runtimeInstance containercontract.Runtime
    capturedErr     error
}

var _ clicontract.Command = (*capturingCommand)(nil)

func (instance *capturingCommand) Run(
    dispatchedRuntime containercontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    instance.capturedErr = instance.Command.Run(instance.runtimeInstance, commandContext)

    return nil
}

type wrappedCipher struct {
    Cipher
}

type accountsNotesBinding struct {
    DefaultCipherReference
}

func (instance accountsNotesBinding) SealBinding() string {
    return "accounts.notes"
}

type accountsBodyBinding struct {
    DefaultCipherReference
}

func (instance accountsBodyBinding) SealBinding() string {
    return "accounts.body"
}

var accountsNotesSealBinding = sealBinding{label: "accounts.notes"}

func scriptedNotesTable(rows [][]driver.Value) []scriptedSqlResponse {
    return []scriptedSqlResponse{
        {
            fragment: "information_schema.COLUMNS",
            columns:  []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"},
            rows:     [][]driver.Value{{"text", int64(65535), int64(65535)}},
        },
        {fragment: "MAX(LENGTH(", columns: []string{"longest"}, rows: [][]driver.Value{{nil}}},
        {fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: rows},
    }
}

func updatedValues(stub *scriptedSqlDriver) map[string]string {
    written := make(map[string]string)

    for _, recorded := range stub.recordedArguments() {
        if false == strings.HasPrefix(recorded.query, "UPDATE ") {
            continue
        }

        written[fmt.Sprint(recorded.arguments[1])] = fmt.Sprint(recorded.arguments[0])
    }

    return written
}

func boundNotesSpec(refuseUnbound bool, deterministic bool) TableSpecification {
    return TableSpecification{
        Table:         "accounts",
        PrimaryKey:    "id",
        Columns:       []string{"notes"},
        Deterministic: deterministic,
        SealBindings:  map[string]ColumnSealBinding{"notes": {Label: "accounts.notes", UnboundAdmitted: false == refuseUnbound}},
    }
}

var migrateModeRuns = map[string]func(migrator *Migrator, specification TableSpecification) (int, error){
    "encrypt": func(migrator *Migrator, specification TableSpecification) (int, error) {
        return migrator.MigrateEncrypt(testRuntime(), specification)
    },
    "reencrypt": func(migrator *Migrator, specification TableSpecification) (int, error) {
        return migrator.MigrateReencrypt(testRuntime(), specification, "v2")
    },
    "decrypt": func(migrator *Migrator, specification TableSpecification) (int, error) {
        return migrator.MigrateDecrypt(testRuntime(), specification)
    },
}

func runQueries(stub *scriptedSqlDriver) []string {
    var queries []string

    for _, recorded := range stub.recorded() {
        if "SELECT version()" != recorded {
            queries = append(queries, recorded)
        }
    }

    return queries
}

type recordingLogger struct {
    mutex   sync.Mutex
    records []recordedLog
}

type recordedLog struct {
    message string
    context loggingcontract.Context
}

func (instance *recordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, recordedLog{message: message, context: context})
}

func (instance *recordingLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}

func (instance *recordingLogger) Info(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}

func (instance *recordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}

func (instance *recordingLogger) Error(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}

func (instance *recordingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}

func (instance *recordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *recordingLogger) CauseChainDepth() int {
    return 8
}

func runCommandLine(t *testing.T, command *EncryptDatabaseCommand, logger loggingcontract.Logger, arguments ...string) error {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    if registerErr := serviceContainer.Register(logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }, container.RegisterOptions{}); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }

    runtimeInstance := runtime.NewRun(context.Background(), serviceContainer.NewScope())

    var runErr error
    engineCommand := &clicontract.CommandContext{
        Name:      command.Name(),
        Flags:     command.Flags(),
        Writer:    io.Discard,
        ErrWriter: io.Discard,
        ExitErrHandler: func(context.Context, *clicontract.CommandContext, error) {
        },
        Action: func(_ context.Context, actionCommand *clicontract.CommandContext) error {
            runErr = command.Run(runtimeInstance, actionCommand)

            return runErr
        },
    }

    _ = engineCommand.Run(context.Background(), append([]string{command.Name()}, arguments...))

    return runErr
}

func newScriptedNotesCommand(t *testing.T, cipherInstance Cipher, rows [][]driver.Value, sealBindings ...SealBindingConfig) (*EncryptDatabaseCommand, *scriptedSqlDriver) {
    t.Helper()

    migrator, stub := newScriptedMigrator(t, scriptedNotesTable(rows))

    return NewEncryptDatabaseCommand(cipherInstance, EncryptDatabaseCommandConfig{Database: migrator.db, SealBindings: sealBindings}), stub
}
