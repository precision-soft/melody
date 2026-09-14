package cli

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
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

/* recordingResetConnector accepts every statement and keeps their text in order: a count select answers zero, which is what a fresh volume holds and what lets the seed and the migration's tolerant steps run, every other select answers no rows, and every exec succeeds. What the reset DID over it is then the recorded sequence, which is what the ordering pins read. */
type recordingResetConnector struct {
    mutex      sync.Mutex
    statements []string
}

func (instance *recordingResetConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return &recordingResetConnection{recorder: instance}, nil
}

func (instance *recordingResetConnector) Driver() driver.Driver {
    return nil
}

func (instance *recordingResetConnector) record(statement string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.statements = append(instance.statements, statement)
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

    instance.recorder.record(query)

    return &recordingResetResult{}, nil
}

func (instance *recordingResetConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
    if nil != ctx.Err() {
        return nil, ctx.Err()
    }

    instance.recorder.record(query)

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
}

func (instance *clearCountingCache) Clear() error {
    instance.mutex.Lock()
    instance.clearCount = instance.clearCount + 1
    onClear := instance.onClear
    instance.mutex.Unlock()

    if nil != onClear {
        onClear()
    }

    return instance.Cache.Clear()
}

func (instance *clearCountingCache) clears() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.clearCount
}

func newResetRuntime(t *testing.T, storage *persistence.CatalogStorage) melodyruntimecontract.Runtime {
    t.Helper()

    runtimeInstance, _ := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorage(nil))

    return runtimeInstance
}

func newResetRuntimeWithArchive(
    t *testing.T,
    storage *persistence.CatalogStorage,
    archiveStorage *persistence.ArchiveStorage,
) (melodyruntimecontract.Runtime, *clearCountingCache) {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceArchiveStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.ArchiveStorage, error) {
            return archiveStorage, nil
        },
    )

    melodycontainer.MustRegister(
        serviceContainer,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
    )

    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceCatalogStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
            return storage, nil
        },
    )

    backend := melodycache.NewInMemoryBackend(0, 0, melodyclock.NewSystemClock())
    cacheInstance := &clearCountingCache{Cache: melodycache.NewManagerOwningBackend(backend, melodycache.NewJsonSerializer())}

    melodycontainer.MustRegister(
        serviceContainer,
        melodycache.ServiceCacheBackend,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
            return backend, nil
        },
    )

    melodycontainer.MustRegister(
        serviceContainer,
        melodycache.ServiceCache,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Cache, error) {
            return cacheInstance, nil
        },
    )

    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer), cacheInstance
}

func newUndialedResetStorage() *persistence.CatalogStorage {
    return persistence.NewCatalogStorage(bun.NewDB(sql.OpenDB(&refusingResetConnector{}), mysqldialect.New()))
}

func newRecordingResetStorage(location string) (*persistence.CatalogStorage, *recordingResetConnector) {
    connector := &recordingResetConnector{}

    return persistence.NewCatalogStorageAt(bun.NewDB(sql.OpenDB(connector), mysqldialect.New()), location), connector
}

/* the refusal is for the environment that wired NEITHER database: an archive wired alone is reset alone, which the test after this one proves */
func TestDatabaseResetCommandRefusesWhenNoDatabaseIsConfigured(t *testing.T) {
    runtimeInstance := newResetRuntime(t, persistence.NewCatalogStorage(nil))

    runErr := NewDatabaseResetCommand().Run(
        runtimeInstance,
        newBoolFlagContext(databaseResetFlagForce, true, nil),
    )
    if nil == runErr {
        t.Fatalf("expected the command to refuse without a configured database")
    }
    if false == strings.Contains(runErr.Error(), "no database configured") {
        t.Fatalf("expected the refusal to name the reason, got %v", runErr)
    }
}

/* the plan is read from the schema rather than written a second time beside it, so a table added to the
   migration and forgotten here would fail this; the trail is named too, because emptying rows of a table
   the set does not own is the half of the reset an operator has to be told about. */
func TestDatabaseResetCommandWithoutForceNamesWhatItWouldDropAndTouchesNothing(t *testing.T) {
    buffer := &bytes.Buffer{}

    runErr := NewDatabaseResetCommand().Run(
        newResetRuntime(t, newUndialedResetStorage()),
        newBoolFlagContext(databaseResetFlagForce, false, buffer),
    )
    if nil != runErr {
        t.Fatalf("expected the command to touch nothing without --force, got %v", runErr)
    }

    output := buffer.String()

    for _, table := range migration.SchemaTableNameList() {
        if false == strings.Contains(output, table) {
            t.Fatalf("expected the plan to name %s, got %q", table, output)
        }
    }

    if false == strings.Contains(output, persistence.AuditTable) {
        t.Fatalf("expected the plan to name the audit trail, got %q", output)
    }

    if false == strings.Contains(output, "nothing was touched") {
        t.Fatalf("expected the command to say it touched nothing, got %q", output)
    }
}

/* the sister of the test above: over the same handle, --force reaches the database and fails on the dial.
   Without it, a fixture that could never fail would let the guard be deleted and leave both green. */
func TestDatabaseResetCommandWithForceReachesTheDatabase(t *testing.T) {
    runErr := NewDatabaseResetCommand().Run(
        newResetRuntime(t, newUndialedResetStorage()),
        newBoolFlagContext(databaseResetFlagForce, true, nil),
    )
    if nil == runErr {
        t.Fatalf("expected --force to reach the undialed database and fail")
    }
}

/* the plan names the archive's tables when there is an archive, and does not when there is not. Both arms
   matter and for different reasons: a plan silent about a database it is about to drop is the failure this
   refusal exists to prevent, and a plan promising to drop a table on a database this environment never
   wired is a lie in the direction an operator would act on. */
func TestDatabaseResetPlanNamesTheArchiveOnlyWhenThereIsOne(t *testing.T) {
    catalogue := newUndialedResetStorage()
    archive := persistence.NewArchiveStorage(newUndialedResetStorage().Database())

    withArchive := strings.Join(databaseResetPlanLineList(catalogue, archive), "\n")
    withoutArchive := strings.Join(databaseResetPlanLineList(catalogue, persistence.NewArchiveStorage(nil)), "\n")

    for _, table := range migration.ArchiveTableNameList() {
        if false == strings.Contains(withArchive, table) {
            t.Fatalf("expected the plan to name %s when an archive is wired, got %q", table, withArchive)
        }

        if true == strings.Contains(withoutArchive, table) {
            t.Fatalf("expected the plan to stay silent about %s when no archive is wired, got %q", table, withoutArchive)
        }
    }

    /* the catalogue's own tables are named on BOTH arms, which is what says the archive half was added
       rather than swapped in */
    for _, table := range migration.SchemaTableNameList() {
        if false == strings.Contains(withoutArchive, table) {
            t.Fatalf("expected the plan to name the catalogue table %s on either arm, got %q", table, withoutArchive)
        }
    }
}

/* the command prints the archive's tables through the same door, over a runtime that carries a wired
   archive: the list reaching the plan is what a mutant can cut, and this is what sees it cut. */
func TestDatabaseResetCommandWithoutForceNamesTheArchiveWhenOneIsWired(t *testing.T) {
    buffer := &bytes.Buffer{}

    runtimeInstance, _ := newResetRuntimeWithArchive(t, newUndialedResetStorage(), persistence.NewArchiveStorage(newUndialedResetStorage().Database()))

    runErr := NewDatabaseResetCommand().Run(
        runtimeInstance,
        newBoolFlagContext(databaseResetFlagForce, false, buffer),
    )
    if nil != runErr {
        t.Fatalf("expected the command to touch nothing without --force, got %v", runErr)
    }

    for _, table := range migration.ArchiveTableNameList() {
        if false == strings.Contains(buffer.String(), table) {
            t.Fatalf("expected the plan to name %s, got %q", table, buffer.String())
        }
    }
}

/* the archive-only environment — MYSQL_HOST blank, PGSQL_HOST set, one of the four combinations the readme promises — used to be refused whole under a message that was false for it. Now the plan names the archive alone, and --force reaches the archive: over an undialed archive handle the refusal is the dial, which is what says the archive half ran. */
func TestDatabaseResetCommandArchiveOnlyEnvironmentResetsTheArchiveAlone(t *testing.T) {
    buffer := &bytes.Buffer{}
    runtimeInstance, _ := newResetRuntimeWithArchive(t, persistence.NewCatalogStorage(nil), persistence.NewArchiveStorageAt(newUndialedResetStorage().Database(), "postgres:5432/melody_example_v3_archive"))

    planErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, false, buffer))
    if nil != planErr {
        t.Fatalf("expected the archive-only plan to be printed, got %v", planErr)
    }

    plan := buffer.String()
    if false == strings.Contains(plan, "postgres:5432/melody_example_v3_archive") {
        t.Fatalf("expected the plan to locate the archive, got %q", plan)
    }
    for _, table := range migration.SchemaTableNameList() {
        if true == strings.Contains(plan, table) {
            t.Fatalf("expected the archive-only plan to stay silent about the catalogue table %s, got %q", table, plan)
        }
    }

    forceErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, nil))
    if nil == forceErr {
        t.Fatalf("expected --force to reach the undialed archive and fail")
    }
    if false == strings.Contains(forceErr.Error(), "archive database at postgres:5432/melody_example_v3_archive") {
        t.Fatalf("expected the failure to name the archive and where it is, got %v", forceErr)
    }
}

/* the catalogue is brought whole — dropped, recreated, trail emptied, reseeded — before the archive is touched: with the archive refusing, every catalogue statement has been recorded, the seed's inserts among them, and the failure names the archive step. The old order returned between the drop and the reseed and left an empty catalogue behind the exit code. */
func TestDatabaseResetCommandReseedsTheCatalogueBeforeTouchingTheArchive(t *testing.T) {
    buffer := &bytes.Buffer{}
    storage, recorder := newRecordingResetStorage("mysql:3306/melody_example_v3")
    runtimeInstance, _ := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorageAt(newUndialedResetStorage().Database(), "postgres:5432/melody_example_v3_archive"))

    runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, buffer))
    if nil == runErr {
        t.Fatalf("expected the undialed archive to fail the run")
    }
    if false == strings.Contains(runErr.Error(), "dropping and recreating the reading archive did not complete on the archive database") {
        t.Fatalf("expected the failure to name the archive step, got %v", runErr)
    }

    inserts := 0
    for _, statement := range recorder.recorded() {
        if true == strings.HasPrefix(strings.ToUpper(statement), "INSERT") && true == strings.Contains(statement, "melody_example_v3_user") {
            inserts++
        }
    }
    if 0 == inserts {
        t.Fatalf("expected the catalogue to have been reseeded before the archive was reached, recorded %v", recorder.recorded())
    }

    output := buffer.String()
    for _, line := range []string{"catalogue reset: the schema was dropped and recreated on mysql:3306/melody_example_v3", "catalogue reset: the audit trail was emptied", "catalogue reset: the nomenclature was reseeded"} {
        if false == strings.Contains(output, line) {
            t.Fatalf("expected the completed step %q to be reported before the failure, got %q", line, output)
        }
    }
}

/* the state a fresh volume holds includes an empty cache, and the clear comes AFTER the reseed: a clear before it would let a reader parked between the two re-cache the rows the reset was about to replace. */
func TestDatabaseResetCommandClearsTheCacheOnceAfterTheReseed(t *testing.T) {
    storage, recorder := newRecordingResetStorage("mysql:3306/melody_example_v3")
    runtimeInstance, cacheInstance := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorage(nil))

    statementsAtClear := -1
    cacheInstance.onClear = func() {
        statementsAtClear = len(recorder.recorded())
    }

    buffer := &bytes.Buffer{}
    runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, buffer))
    if nil != runErr {
        t.Fatalf("expected the reset over the recording handle to complete, got %v", runErr)
    }

    if 1 != cacheInstance.clears() {
        t.Fatalf("expected exactly one clear of the cache, got %d", cacheInstance.clears())
    }

    if statementsAtClear != len(recorder.recorded()) {
        t.Fatalf("expected the clear to come after the last statement (%d), it came at %d", len(recorder.recorded()), statementsAtClear)
    }

    if false == strings.Contains(buffer.String(), "cache cleared: this process's own entries") {
        t.Fatalf("expected the command to say whose cache it cleared, got %q", buffer.String())
    }
}

/* the drops run under the runtime's context: cancelled, the first statement is refused and nothing is recorded, where a background context let a reset ignore the one signal every other command honours. */
func TestDatabaseResetCommandHonoursTheRuntimeContext(t *testing.T) {
    storage, recorder := newRecordingResetStorage("mysql:3306/melody_example_v3")
    runtimeInstance, _ := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorage(nil))

    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    cancelledRuntime := melodyruntime.New(ctx, runtimeInstance.Container().NewScope(), runtimeInstance.Container())

    runErr := NewDatabaseResetCommand().Run(cancelledRuntime, newBoolFlagContext(databaseResetFlagForce, true, nil))
    if nil == runErr {
        t.Fatalf("expected the cancelled context to refuse the reset")
    }

    /* the dialect's own version discovery runs on the handle's construction, under a context of its own, so it is the one statement a cancelled reset still records */
    for _, statement := range recorder.recorded() {
        if false == strings.Contains(statement, "version()") {
            t.Fatalf("expected no statement of the reset under a cancelled context, recorded %v", recorder.recorded())
        }
    }
}


func TestResetInvalidatesCacheWhenDatabaseWorkFails(t *testing.T) {
    runtimeInstance, cacheInstance := newResetRuntimeWithArchive(t, newUndialedResetStorage(), persistence.NewArchiveStorage(nil))
    err := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, nil))
    if nil == err { t.Fatal("expected the database refusal") }
    if 1 != cacheInstance.clearCount { t.Fatalf("cache clears=%d; want one even after database failure", cacheInstance.clearCount) }
}
