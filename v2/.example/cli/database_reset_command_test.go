package cli

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v2/.example/migration"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

const (
    testResetDatabaseServiceName = "service.test.reset.database"
    testResetDatabaseLocation    = "mysql:3306/melody_example_v2"
)

/* refusingResetConnector is a driver that refuses every connection, which is what makes the pair of tests
   below a proof rather than a pair of green runs: without --force the command must answer nil over this
   handle, and WITH --force it must fail, because the first statement has to dial. The failing arm is the
   sister that says the nil of the other one came from the guard and not from a fixture that cannot fail. */
type refusingResetConnector struct{}

func (instance *refusingResetConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("this handle is never dialed")
}

func (instance *refusingResetConnector) Driver() driver.Driver {
    return nil
}

/* recordingResetConnector accepts every statement and keeps their text in order: a count select answers zero, which is what a fresh volume holds and what lets the seed and the migration's tolerant steps run, every other select answers no rows, and every exec succeeds — unless the statement matches refuseMatching, which is how one step is made to fail. What the reset DID over it is then the recorded sequence, which is what the ordering pins read. */
type recordingResetConnector struct {
    mutex          sync.Mutex
    statements     []string
    refuseMatching string
}

func (instance *recordingResetConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return &recordingResetConnection{recorder: instance}, nil
}

func (instance *recordingResetConnector) Driver() driver.Driver {
    return nil
}

func (instance *recordingResetConnector) record(statement string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if "" != instance.refuseMatching && true == strings.Contains(statement, instance.refuseMatching) {
        return errors.New("the server refused: " + instance.refuseMatching)
    }

    instance.statements = append(instance.statements, statement)

    return nil
}

func (instance *recordingResetConnector) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.statements...)
}

type recordingResetConnection struct {
    recorder *recordingResetConnector
}

func (instance *recordingResetConnection) Prepare(query string) (driver.Stmt, error) {
    return nil, errors.New("prepared statements are not supported by the recording driver")
}

func (instance *recordingResetConnection) Close() error {
    return nil
}

func (instance *recordingResetConnection) Begin() (driver.Tx, error) {
    return nil, errors.New("transactions are not supported by the recording driver")
}

func (instance *recordingResetConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
    if nil != ctx.Err() {
        return nil, ctx.Err()
    }

    if recordErr := instance.recorder.record(query); nil != recordErr {
        return nil, recordErr
    }

    return &recordingResetResult{}, nil
}

func (instance *recordingResetConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
    if nil != ctx.Err() {
        return nil, ctx.Err()
    }

    if recordErr := instance.recorder.record(query); nil != recordErr {
        return nil, recordErr
    }

    if true == strings.Contains(strings.ToLower(query), "count(") {
        return &recordingResetRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}, nil
    }

    return &recordingResetRows{columns: []string{}}, nil
}

type recordingResetResult struct{}

func (instance *recordingResetResult) LastInsertId() (int64, error) {
    return 1, nil
}

func (instance *recordingResetResult) RowsAffected() (int64, error) {
    return 1, nil
}

type recordingResetRows struct {
    columns []string
    rows    [][]driver.Value
    cursor  int
}

func (instance *recordingResetRows) Columns() []string {
    return instance.columns
}

func (instance *recordingResetRows) Close() error {
    return nil
}

func (instance *recordingResetRows) Next(destination []driver.Value) error {
    if instance.cursor >= len(instance.rows) {
        return io.EOF
    }

    copy(destination, instance.rows[instance.cursor])
    instance.cursor = instance.cursor + 1

    return nil
}

/* clearCountingCache is the cache the reset clears: it counts the clears and keeps the order they came in relative to the recorded statements, so the pin can say the clear came AFTER the reseed rather than merely that it happened. */
type clearCountingCache struct {
    melodycachecontract.Cache

    mutex      sync.Mutex
    clearCount int
    onClear    func()
    refusal    error
}

func (instance *clearCountingCache) Clear() error {
    instance.mutex.Lock()
    instance.clearCount = instance.clearCount + 1
    onClear := instance.onClear
    refusal := instance.refusal
    instance.mutex.Unlock()

    if nil != onClear {
        onClear()
    }

    if nil != refusal {
        return refusal
    }

    return instance.Cache.Clear()
}

func (instance *clearCountingCache) clears() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.clearCount
}

func newUndialedResetDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(&refusingResetConnector{}), mysqldialect.New())
}

func newResetCommandContainer(t *testing.T) melodycontainercontract.Container {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()
    registerCliTestService[*bun.DB](serviceContainer, testResetDatabaseServiceName, newUndialedResetDatabase())

    backend := melodycache.NewInMemoryBackend(0, 0, melodyclock.NewSystemClock())
    cacheInstance := &clearCountingCache{Cache: melodycache.NewManagerOwningBackend(backend, melodycache.NewJsonSerializer())}
    t.Cleanup(func() {
        _ = cacheInstance.Cache.Close()
    })

    registerCliTestService[melodycachecontract.Backend](serviceContainer, melodycache.ServiceCacheBackend, backend)
    registerCliTestService[melodycachecontract.Cache](serviceContainer, melodycache.ServiceCache, cacheInstance)

    return serviceContainer
}

/* newRecordingResetContainer wires the handle over the recording driver and the cache the reset clears — the in-memory backend under the framework's own name, so the scope line reads the wiring the way the composition root registers it. */
func newRecordingResetContainer(t *testing.T, recorder *recordingResetConnector) (melodycontainercontract.Container, *clearCountingCache) {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()
    registerCliTestService[*bun.DB](serviceContainer, testResetDatabaseServiceName, bun.NewDB(sql.OpenDB(recorder), mysqldialect.New()))

    backend := melodycache.NewInMemoryBackend(0, 0, melodyclock.NewSystemClock())
    cacheInstance := &clearCountingCache{Cache: melodycache.NewManagerOwningBackend(backend, melodycache.NewJsonSerializer())}
    t.Cleanup(func() {
        _ = cacheInstance.Cache.Close()
    })

    registerCliTestService[melodycachecontract.Backend](serviceContainer, melodycache.ServiceCacheBackend, backend)
    registerCliTestService[melodycachecontract.Cache](serviceContainer, melodycache.ServiceCache, cacheInstance)

    return serviceContainer, cacheInstance
}

func TestDatabaseResetCommandRefusesWhenNoDatabaseIsConfigured(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer()

    _, runErr := runCliCommand(
        NewDatabaseResetCommand("", ""),
        newCliTestRuntime(serviceContainer),
        []string{"--force"},
    )
    if nil == runErr {
        t.Fatalf("expected the command to refuse without a configured database")
    }
    if false == strings.Contains(runErr.Error(), "no database configured") {
        t.Fatalf("expected the refusal to name the reason, got %v", runErr)
    }
}

/* the plan is read from the schema rather than written a second time beside it, so a table added to the
   migration and forgotten here would fail this; and it names the database the tables live in, the one
   line that separates a reset of this example's volume from a reset of whatever the host points at. */
func TestDatabaseResetCommandWithoutForceNamesEveryTableAndTheDatabaseAndTouchesNothing(t *testing.T) {
    serviceContainer := newResetCommandContainer(t)

    output, runErr := runCliCommand(
        NewDatabaseResetCommand(testResetDatabaseServiceName, testResetDatabaseLocation),
        newCliTestRuntime(serviceContainer),
        nil,
    )
    if nil != runErr {
        t.Fatalf("expected the command to touch nothing without --force, got %v", runErr)
    }

    for _, table := range migration.SchemaTableNameList() {
        if false == strings.Contains(output, table) {
            t.Fatalf("expected the plan to name %s, got %q", table, output)
        }
    }

    if false == strings.Contains(output, "the migration set owns on "+testResetDatabaseLocation+":") {
        t.Fatalf("expected the plan to name the database, got %q", output)
    }

    if false == strings.Contains(output, "nothing was touched") {
        t.Fatalf("expected the command to say it touched nothing, got %q", output)
    }
}

/* the sister of the test above: over the same handle, --force reaches the database and fails on the dial.
   Without it, a fixture that could never fail would let the guard be deleted and leave both green. */
func TestDatabaseResetCommandWithForceReachesTheDatabase(t *testing.T) {
    serviceContainer := newResetCommandContainer(t)

    _, runErr := runCliCommand(
        NewDatabaseResetCommand(testResetDatabaseServiceName, testResetDatabaseLocation),
        newCliTestRuntime(serviceContainer),
        []string{"--force"},
    )
    if nil == runErr {
        t.Fatalf("expected --force to reach the undialed database and fail")
    }

    /* the failure names the step and the database it did not complete on: the driver's refusal alone told the operator neither */
    if false == strings.Contains(runErr.Error(), "dropping and recreating the schema did not complete on the database at "+testResetDatabaseLocation) {
        t.Fatalf("expected the failure to name the step and the database, got %v", runErr)
    }
}

/* the state a fresh volume holds includes an empty cache, and the clear comes AFTER the reseed: a clear before it would let a reader parked between the two re-cache the rows the reset was about to replace; a reseed that is refused clears nothing */
func TestDatabaseResetCommandClearsTheCacheOnceAfterTheReseed(t *testing.T) {
    catalogRecorder := &recordingResetConnector{}
    serviceContainer, cacheInstance := newRecordingResetContainer(t, catalogRecorder)

    statementsAtClear := -1
    cacheInstance.onClear = func() {
        statementsAtClear = len(catalogRecorder.recorded())
    }

    output, runErr := runCliCommand(NewDatabaseResetCommand(testResetDatabaseServiceName, testResetDatabaseLocation), newCliTestRuntime(serviceContainer), []string{"--force"})
    if nil != runErr {
        t.Fatalf("expected the reset over the recording handle to complete, got %v", runErr)
    }

    if 1 != cacheInstance.clears() {
        t.Fatalf("expected exactly one clear of the cache, got %d", cacheInstance.clears())
    }

    if statementsAtClear != len(catalogRecorder.recorded()) {
        t.Fatalf("expected the clear to come after the catalogue's last statement (%d), it came at %d", len(catalogRecorder.recorded()), statementsAtClear)
    }

    for _, line := range []string{
        "database reset: the schema was dropped and recreated on " + testResetDatabaseLocation,
        "database reset: the nomenclature was reseeded",
        "cache cleared: this process's own entries",
    } {
        if false == strings.Contains(output, line) {
            t.Fatalf("expected the step line %q, got %q", line, output)
        }
    }

    /* the migration set records itself with an INSERT of its own, so the refusal is on the seed rows alone */
    refusedRecorder := &recordingResetConnector{refuseMatching: "INSERT IGNORE INTO `melody_example_v2_"}
    refusedContainer, refusedCache := newRecordingResetContainer(t, refusedRecorder)

    _, refusedErr := runCliCommand(NewDatabaseResetCommand(testResetDatabaseServiceName, testResetDatabaseLocation), newCliTestRuntime(refusedContainer), []string{"--force"})
    if nil == refusedErr || false == strings.Contains(refusedErr.Error(), "reseeding the nomenclature did not complete on the database at "+testResetDatabaseLocation) {
        t.Fatalf("expected the refused reseed to name its step and database, got %v", refusedErr)
    }

    if 1 != refusedCache.clears() {
        t.Fatalf("expected a refused reseed to clear the cache once, since the schema under it was already dropped, got %d", refusedCache.clears())
    }
}

/* the drops run under the runtime's context: cancelled, the first statement is refused and nothing is recorded, so a reset honours the signal every other command honours. */
func TestDatabaseResetCommandHonoursTheRuntimeContext(t *testing.T) {
    catalogRecorder := &recordingResetConnector{}
    serviceContainer, _ := newRecordingResetContainer(t, catalogRecorder)

    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    cancelledRuntime := melodyruntime.New(ctx, serviceContainer.NewScope(), serviceContainer)

    _, runErr := runCliCommand(NewDatabaseResetCommand(testResetDatabaseServiceName, testResetDatabaseLocation), cancelledRuntime, []string{"--force"})
    if nil == runErr {
        t.Fatalf("expected the cancelled context to refuse the reset")
    }

    /* the dialect's own version discovery runs on the handle's construction, under a context of its own, so it is the one statement a cancelled reset still records */
    for _, statement := range catalogRecorder.recorded() {
        if false == strings.Contains(strings.ToLower(statement), "version") {
            t.Fatalf("expected no statement of the reset under a cancelled context, recorded %v", catalogRecorder.recorded())
        }
    }
}

var _ melodyruntimecontract.Runtime = (melodyruntimecontract.Runtime)(nil)

func TestDatabaseResetCommandJoinsAFailedClearToTheStepThatFailed(t *testing.T) {
    refusedRecorder := &recordingResetConnector{refuseMatching: "INSERT IGNORE INTO `melody_example_v2_"}
    refusedContainer, refusedCache := newRecordingResetContainer(t, refusedRecorder)
    cacheRefusal := errors.New("the cache refused the clear")
    refusedCache.refusal = cacheRefusal

    _, runErr := runCliCommand(NewDatabaseResetCommand(testResetDatabaseServiceName, testResetDatabaseLocation), newCliTestRuntime(refusedContainer), []string{"--force"})
    if nil == runErr || false == strings.HasPrefix(runErr.Error(), "database reset: reseeding the nomenclature did not complete") || false == errors.Is(runErr, cacheRefusal) {
        t.Fatalf("expected the reseed's failure first with the failed clear joined, got %v", runErr)
    }
}

func TestDatabaseResetCommandRefusesBeforeTouchingAnythingWhenTheCacheCannotBeResolved(t *testing.T) {
    recorder := &recordingResetConnector{}
    serviceContainer := melodycontainer.NewContainer()
    registerCliTestService[*bun.DB](serviceContainer, testResetDatabaseServiceName, bun.NewDB(sql.OpenDB(recorder), mysqldialect.New()))

    _, runErr := runCliCommand(NewDatabaseResetCommand(testResetDatabaseServiceName, testResetDatabaseLocation), newCliTestRuntime(serviceContainer), []string{"--force"})
    if nil == runErr {
        t.Fatalf("expected the reset to refuse a cache it cannot resolve")
    }

    /* the driver's own version probe on connect writes nothing; every statement of the reset does */
    for _, statement := range recorder.recorded() {
        if "SELECT version()" != statement {
            t.Fatalf("expected nothing touched before the cache was resolved, got %q", recorder.recorded())
        }
    }
}
