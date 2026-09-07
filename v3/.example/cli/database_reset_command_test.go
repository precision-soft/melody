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

    serviceContainer := melodycontainer.NewContainer()

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
