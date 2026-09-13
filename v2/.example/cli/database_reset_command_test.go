package cli

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/.example/migration"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

const testResetDatabaseServiceName = "service.test.reset.database"

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

func newUndialedResetDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(&refusingResetConnector{}), mysqldialect.New())
}

func newResetCommandContainer(t *testing.T) melodycontainercontract.Container {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()
    registerCliTestService[*bun.DB](serviceContainer, testResetDatabaseServiceName, newUndialedResetDatabase())

    return serviceContainer
}

func TestDatabaseResetCommandRefusesWhenNoDatabaseIsConfigured(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer()

    _, runErr := runCliCommand(
        NewDatabaseResetCommand(""),
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
   migration and forgotten here would fail this. */
func TestDatabaseResetCommandWithoutForceNamesEveryTableAndTouchesNothing(t *testing.T) {
    serviceContainer := newResetCommandContainer(t)

    output, runErr := runCliCommand(
        NewDatabaseResetCommand(testResetDatabaseServiceName),
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

    if false == strings.Contains(output, "nothing was touched") {
        t.Fatalf("expected the command to say it touched nothing, got %q", output)
    }
}

/* the sister of the test above: over the same handle, --force reaches the database and fails on the dial.
   Without it, a fixture that could never fail would let the guard be deleted and leave both green. */
func TestDatabaseResetCommandWithForceReachesTheDatabase(t *testing.T) {
    serviceContainer := newResetCommandContainer(t)

    _, runErr := runCliCommand(
        NewDatabaseResetCommand(testResetDatabaseServiceName),
        newCliTestRuntime(serviceContainer),
        []string{"--force"},
    )
    if nil == runErr {
        t.Fatalf("expected --force to reach the undialed database and fail")
    }
}
