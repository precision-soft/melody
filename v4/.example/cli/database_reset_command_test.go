package cli

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
    "testing"

    melodyaudit "github.com/precision-soft/melody/integrations/bunorm/v4/audit"
    "github.com/precision-soft/melody/v4/.example/migration"
    "github.com/precision-soft/melody/v4/.example/persistence"
    examplesecurity "github.com/precision-soft/melody/v4/.example/security"
    "github.com/precision-soft/melody/v4/.example/service"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

type refusingResetConnector struct{}

func (instance *refusingResetConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("this handle is never dialed")
}

func (instance *refusingResetConnector) Driver() driver.Driver {
    return nil
}

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
    instance.recorder.record("BEGIN")

    return &recordingResetTransaction{recorder: instance.recorder}, nil
}

type recordingResetTransaction struct {
    recorder *recordingResetConnector
}

func (instance *recordingResetTransaction) Commit() error {
    instance.recorder.record("COMMIT")

    return nil
}

func (instance *recordingResetTransaction) Rollback() error {
    instance.recorder.record("ROLLBACK")

    return nil
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

func newResetRuntime(t *testing.T, storage *persistence.CatalogStorage) melodycontainercontract.Runtime {
    t.Helper()

    runtimeInstance, _ := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorage(nil))

    return runtimeInstance
}

func newUndialedResetStorage() *persistence.CatalogStorage {
    return persistence.NewCatalogStorage(bun.NewDB(sql.OpenDB(&refusingResetConnector{}), mysqldialect.New()))
}

func newRecordingResetStorage(location string) (*persistence.CatalogStorage, *recordingResetConnector) {
    connector := &recordingResetConnector{}

    return persistence.NewCatalogStorageAt(bun.NewDB(sql.OpenDB(connector), mysqldialect.New()), location).WithAccountSeed(), connector
}

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

func TestDatabaseResetCommandWithForceReachesTheDatabase(t *testing.T) {
    runErr := NewDatabaseResetCommand().Run(
        newResetRuntime(t, persistence.NewCatalogStorageAt(newUndialedResetStorage().Database(), "mysql:3306/melody_example_v4")),
        newBoolFlagContext(databaseResetFlagForce, true, nil),
    )
    if nil == runErr {
        t.Fatalf("expected --force to reach the undialed database and fail")
    }

    if false == strings.Contains(runErr.Error(), "database reset: dropping and recreating the schema did not complete on the catalogue database at mysql:3306/melody_example_v4") {
        t.Fatalf("expected the failure to be the drop step's on the catalogue, got %v", runErr)
    }

    if false == strings.Contains(fmt.Sprintf("%v", exception.LogContext(runErr, exception.LogContextOptions{})), "this handle is never dialed") {
        t.Fatalf("expected the drop step to have failed on the dial itself, got %v", exception.LogContext(runErr, exception.LogContextOptions{}))
    }
}

func TestDatabaseResetCommandWithForceDropsThenEmptiesTheTrailThenReseeds(t *testing.T) {
    storage, recorder := newRecordingResetStorage("mysql:3306/melody_example_v4")
    runtimeInstance, _ := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorage(nil))

    if runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, &bytes.Buffer{})); nil != runErr {
        t.Fatalf("expected the reset over the recording handle to complete, got %v", runErr)
    }

    statements := recorder.recorded()
    indexOf := func(matches func(statement string) bool) int {
        for index, statement := range statements {
            if true == matches(statement) {
                return index
            }
        }

        return -1
    }

    lastDrop := -1
    for index, statement := range statements {
        if true == strings.HasPrefix(strings.ToUpper(statement), "DROP TABLE") {
            lastDrop = index
        }
    }

    auditDelete := indexOf(func(statement string) bool {
        return "DELETE FROM `"+persistence.AuditTable+"`" == statement
    })
    transactionDelete := indexOf(func(statement string) bool {
        return "DELETE FROM `"+melodyaudit.DefaultTransactionTable+"`" == statement
    })
    firstSeed := indexOf(func(statement string) bool {
        return true == strings.HasPrefix(strings.ToUpper(statement), "INSERT") && true == strings.Contains(statement, "melody_example_v4_user")
    })

    if -1 == lastDrop || -1 == auditDelete || -1 == transactionDelete || -1 == firstSeed {
        t.Fatalf("expected a drop, both trail deletes and a seed, got drop %d, audit %d, transaction %d, seed %d in %q", lastDrop, auditDelete, transactionDelete, firstSeed, statements)
    }

    if false == (lastDrop < auditDelete && lastDrop < transactionDelete) {
        t.Fatalf("expected the trail to be emptied after the schema was dropped, got drop %d, audit %d, transaction %d", lastDrop, auditDelete, transactionDelete)
    }

    if false == (auditDelete < firstSeed && transactionDelete < firstSeed) {
        t.Fatalf("expected the nomenclature to be reseeded after the trail was emptied, got audit %d, transaction %d, seed %d", auditDelete, transactionDelete, firstSeed)
    }
}

func TestDatabaseResetPlanNamesTheArchiveOnlyWhenThereIsOne(t *testing.T) {
    catalogue := newUndialedResetStorage()
    archive := persistence.NewArchiveStorage(newUndialedResetStorage().Database())

    withArchive := strings.Join(databaseResetPlanLineList(catalogue, archive, nil), "\n")
    withoutArchive := strings.Join(databaseResetPlanLineList(catalogue, persistence.NewArchiveStorage(nil), nil), "\n")

    for _, table := range migration.ArchiveTableNameList() {
        if false == strings.Contains(withArchive, table) {
            t.Fatalf("expected the plan to name %s when an archive is wired, got %q", table, withArchive)
        }

        if true == strings.Contains(withoutArchive, table) {
            t.Fatalf("expected the plan to stay silent about %s when no archive is wired, got %q", table, withoutArchive)
        }
    }

    for _, table := range migration.SchemaTableNameList() {
        if false == strings.Contains(withoutArchive, table) {
            t.Fatalf("expected the plan to name the catalogue table %s on either arm, got %q", table, withoutArchive)
        }
    }
}

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

func TestDatabaseResetCommandArchiveOnlyEnvironmentResetsTheArchiveAlone(t *testing.T) {
    buffer := &bytes.Buffer{}
    runtimeInstance, _ := newResetRuntimeWithArchive(t, persistence.NewCatalogStorage(nil), persistence.NewArchiveStorageAt(newUndialedResetStorage().Database(), "postgres:5432/melody_example_v4_archive"))

    planErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, false, buffer))
    if nil != planErr {
        t.Fatalf("expected the archive-only plan to be printed, got %v", planErr)
    }

    plan := buffer.String()
    if false == strings.Contains(plan, "postgres:5432/melody_example_v4_archive") {
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
    if false == strings.Contains(forceErr.Error(), "archive database at postgres:5432/melody_example_v4_archive") {
        t.Fatalf("expected the failure to name the archive and where it is, got %v", forceErr)
    }
}

func TestDatabaseResetCommandReseedsTheCatalogueBeforeTouchingTheArchive(t *testing.T) {
    buffer := &bytes.Buffer{}
    storage, recorder := newRecordingResetStorage("mysql:3306/melody_example_v4")
    runtimeInstance, cacheInstance := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorageAt(newUndialedResetStorage().Database(), "postgres:5432/melody_example_v4_archive"))

    runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, buffer))
    if nil == runErr {
        t.Fatalf("expected the undialed archive to fail the run")
    }
    if false == strings.Contains(runErr.Error(), "dropping and recreating the reading archive did not complete on the archive database") {
        t.Fatalf("expected the failure to name the archive step, got %v", runErr)
    }

    inserts := 0
    for _, statement := range recorder.recorded() {
        if true == strings.HasPrefix(strings.ToUpper(statement), "INSERT") && true == strings.Contains(statement, "melody_example_v4_user") {
            inserts++
        }
    }
    if 0 == inserts {
        t.Fatalf("expected the catalogue to have been reseeded before the archive was reached, recorded %v", recorder.recorded())
    }

    output := buffer.String()
    for _, line := range []string{"catalogue reset: the schema was dropped and recreated on mysql:3306/melody_example_v4", "catalogue reset: the audit trail was emptied", "catalogue reset: the nomenclature was reseeded"} {
        if false == strings.Contains(output, line) {
            t.Fatalf("expected the completed step %q to be reported before the failure, got %q", line, output)
        }
    }

    if 1 != cacheInstance.clears() {
        t.Fatalf("expected the cache to be cleared once before the archive was reached, got %d clears", cacheInstance.clears())
    }

    if false == strings.Contains(output, "cache cleared") {
        t.Fatalf("expected the clear to be reported before the archive's failure, got %q", output)
    }
}

func TestDatabaseResetCommandClearsTheCacheOnceAfterTheReseed(t *testing.T) {
    storage, recorder := newRecordingResetStorage("mysql:3306/melody_example_v4")
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

func TestDatabaseResetCommandHonoursTheRuntimeContext(t *testing.T) {
    storage, recorder := newRecordingResetStorage("mysql:3306/melody_example_v4")
    runtimeInstance, _ := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorage(nil))

    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    cancelledRuntime, childErr := melodyruntime.NewChildRun(runtimeInstance, ctx)
    if nil != childErr {
        t.Fatalf("expected the cancelled child run, got %v", childErr)
    }

    runErr := NewDatabaseResetCommand().Run(cancelledRuntime, newBoolFlagContext(databaseResetFlagForce, true, nil))
    if nil == runErr {
        t.Fatalf("expected the cancelled context to refuse the reset")
    }

    /* the dialect's version discovery runs at the handle's construction under its own context, so a cancelled reset still records it */
    for _, statement := range recorder.recorded() {
        if false == strings.Contains(statement, "version()") {
            t.Fatalf("expected no statement of the reset under a cancelled context, recorded %v", recorder.recorded())
        }
    }
}

func TestDatabaseResetCommandArchiveOnlyEnvironmentClearsTheCacheToo(t *testing.T) {
    archiveStorage, _ := newRecordingResetStorage("postgres:5432/melody_example_v4_archive")
    runtimeInstance, cacheInstance := newResetRuntimeWithArchive(t, persistence.NewCatalogStorage(nil), persistence.NewArchiveStorageAt(archiveStorage.Database(), "postgres:5432/melody_example_v4_archive"))

    buffer := &bytes.Buffer{}
    if runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, buffer)); nil != runErr {
        t.Fatalf("expected the archive-only reset over the recording handle to complete, got %v", runErr)
    }

    if 1 != cacheInstance.clears() || false == strings.Contains(buffer.String(), "cache cleared:") {
        t.Fatalf("expected the archive-only reset to clear the cache once and say so, got %d clears and %q", cacheInstance.clears(), buffer.String())
    }
}

func TestDatabaseResetCommandNamesTheUntouchedArchiveWhenTheClearFails(t *testing.T) {
    storage, _ := newRecordingResetStorage("mysql:3306/melody_example_v4")
    archiveStorage, archiveRecorder := newRecordingResetStorage("postgres:5432/melody_example_v4_archive")
    runtimeInstance, cacheInstance := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorageAt(archiveStorage.Database(), "postgres:5432/melody_example_v4_archive"))
    cacheInstance.refusal = errors.New("redis: connection refused")

    runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, nil))
    if nil == runErr || false == strings.Contains(runErr.Error(), "the archive was not touched") {
        t.Fatalf("expected the failed clear to name the untouched archive, got %v", runErr)
    }

    for _, statement := range archiveRecorder.recorded() {
        if false == strings.Contains(strings.ToLower(statement), "version") {
            t.Fatalf("expected the archive untouched after the failed clear, recorded %v", archiveRecorder.recorded())
        }
    }
}

func TestDatabaseResetCommandClearsTheCacheWhenAStepFailsAfterTheResetBegan(t *testing.T) {
    runtimeInstance, cacheInstance := newResetRuntimeWithArchive(
        t,
        persistence.NewCatalogStorageAt(newUndialedResetStorage().Database(), "mysql:3306/melody_example_v4"),
        persistence.NewArchiveStorage(nil),
    )

    runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, &bytes.Buffer{}))
    if nil == runErr || false == strings.Contains(runErr.Error(), "dropping and recreating the schema did not complete") {
        t.Fatalf("expected the drop step's failure, got %v", runErr)
    }

    if 1 != cacheInstance.clears() {
        t.Fatalf("expected the failed reset to clear the cache once, got %d clears", cacheInstance.clears())
    }
}

func TestDatabaseResetCommandJoinsAFailedClearToTheStepThatFailed(t *testing.T) {
    runtimeInstance, cacheInstance := newResetRuntimeWithArchive(
        t,
        persistence.NewCatalogStorageAt(newUndialedResetStorage().Database(), "mysql:3306/melody_example_v4"),
        persistence.NewArchiveStorage(nil),
    )
    cacheRefusal := errors.New("the cache refused the clear")
    cacheInstance.refusal = cacheRefusal

    runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, &bytes.Buffer{}))
    if nil == runErr || false == strings.HasPrefix(runErr.Error(), "database reset: dropping and recreating the schema did not complete") || false == errors.Is(runErr, cacheRefusal) {
        t.Fatalf("expected the drop step's failure first with the failed clear joined, got %v", runErr)
    }
}

func TestDatabaseResetCommandRefusesBeforeTouchingAnythingWhenTheCacheCannotBeResolved(t *testing.T) {
    storage, recorder := newRecordingResetStorage("mysql:3306/melody_example_v4")

    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceCatalogStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.CatalogStorage, error) {
            return storage, nil
        },
        melodycontainer.RegisterOptions{},
    )
    melodycontainer.MustRegister(
        serviceContainer,
        persistence.ServiceArchiveStorage,
        func(resolver melodycontainercontract.Resolver) (*persistence.ArchiveStorage, error) {
            return persistence.NewArchiveStorage(nil), nil
        },
        melodycontainer.RegisterOptions{},
    )
    runtimeInstance := melodyruntime.NewRun(context.Background(), serviceContainer.NewScope())

    runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, &bytes.Buffer{}))
    if nil == runErr {
        t.Fatalf("expected the reset to refuse a cache it cannot resolve")
    }

    for _, statement := range recorder.recorded() {
        if "SELECT version()" != statement {
            t.Fatalf("expected nothing touched before the cache was resolved, got %q", recorder.recorded())
        }
    }
}

func TestDatabaseResetCommandGroupsTheReseedUnderOneAuditTransaction(t *testing.T) {
    storage, recorder := newRecordingResetStorage("mysql:3306/melody_example_v4")
    runtimeInstance, _ := newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorage(nil))

    if runErr := NewDatabaseResetCommand().Run(runtimeInstance, newBoolFlagContext(databaseResetFlagForce, true, &bytes.Buffer{})); nil != runErr {
        t.Fatalf("expected the reset over the recording handle to complete, got %v", runErr)
    }

    actor := service.ActorFromRuntime(runtimeInstance)
    if "" == actor {
        t.Fatal("expected the reset's runtime to name an actor")
    }

    transactionOpened := -1
    firstSeededRow := -1
    groupedEntries := 0
    for index, statement := range recorder.recorded() {
        if -1 == transactionOpened && true == strings.HasPrefix(statement, "INSERT INTO `"+melodyaudit.DefaultTransactionTable+"`") && true == strings.Contains(statement, "DEFAULT, '"+actor+`', '{"command":"example:db:reset"}'`) {
            transactionOpened = index
        }

        if -1 == firstSeededRow && (true == strings.HasPrefix(statement, "INSERT INTO `melody_example_v4_product`") || true == strings.HasPrefix(statement, "INSERT INTO `melody_example_v4_user`")) {
            firstSeededRow = index
        }

        /* the recording driver answers every insert's id as 1, so the transaction's id is 1 */
        if true == strings.HasPrefix(statement, "INSERT INTO "+persistence.AuditTable+" ") && true == strings.Contains(statement, "VALUES (DEFAULT, 1, ") && true == strings.Contains(statement, "'INSERT'") {
            groupedEntries++
        }
    }

    if -1 == transactionOpened || -1 == firstSeededRow || transactionOpened > firstSeededRow {
        t.Fatalf("expected the audit transaction naming the actor and the command opened before the first seeded row, got transaction %d, first row %d", transactionOpened, firstSeededRow)
    }

    if 8 != groupedEntries {
        t.Fatalf("expected the five products and three users seeded each with an insert entry in the transaction, got %d", groupedEntries)
    }
}

func TestDatabaseResetPlanNamesWhatHappensToTheDeviceTokens(t *testing.T) {
    catalogue := newUndialedResetStorage()

    shared := strings.Join(databaseResetPlanLineList(catalogue, persistence.NewArchiveStorage(nil), examplesecurity.NewTokenNamespace(nil, "melody-example-v4:token")), "\n")
    if false == strings.Contains(shared, "every device token and revocation epoch of the token store on the shared redis") {
        t.Fatalf("expected the plan to name the release of the device tokens, got %q", shared)
    }

    local := strings.Join(databaseResetPlanLineList(catalogue, persistence.NewArchiveStorage(nil), nil), "\n")
    if false == strings.Contains(local, "they stay valid there until it restarts") || true == strings.Contains(local, "every device token and revocation epoch") {
        t.Fatalf("expected the plan to say the device tokens stay in the server's memory, got %q", local)
    }
}
