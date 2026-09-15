package cli

import (
    "bytes"
    "context"
    "strings"
    "testing"
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

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
        newResetRuntime(t, newUndialedResetStorage()),
        newBoolFlagContext(databaseResetFlagForce, true, nil),
    )
    if nil == runErr {
        t.Fatalf("expected --force to reach the undialed database and fail")
    }
}

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
