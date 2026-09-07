package cli

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/persistence"
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

func newResetRuntime(t *testing.T, storage *persistence.CatalogStorage) melodyruntimecontract.Runtime {
    t.Helper()

    return newResetRuntimeWithArchive(t, storage, persistence.NewArchiveStorage(nil))
}

func newResetRuntimeWithArchive(
    t *testing.T,
    storage *persistence.CatalogStorage,
    archiveStorage *persistence.ArchiveStorage,
) melodyruntimecontract.Runtime {
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

    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func newUndialedResetStorage() *persistence.CatalogStorage {
    return persistence.NewCatalogStorage(bun.NewDB(sql.OpenDB(&refusingResetConnector{}), mysqldialect.New()))
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
    withArchive := strings.Join(databaseResetPlanLineList(true), "\n")
    withoutArchive := strings.Join(databaseResetPlanLineList(false), "\n")

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

    runErr := NewDatabaseResetCommand().Run(
        newResetRuntimeWithArchive(t, newUndialedResetStorage(), persistence.NewArchiveStorage(newUndialedResetStorage().Database())),
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
