package config

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "strings"
    "sync/atomic"
    "testing"

    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

/* countingRefusingConnector refuses every connection and counts the attempts, so a test reads how many times
   the store's migration went to the database */
type countingRefusingConnector struct {
    attempts atomic.Int64
}

func (instance *countingRefusingConnector) Connect(ctx context.Context) (driver.Conn, error) {
    instance.attempts.Add(1)

    return nil, errors.New("the catalogue database is down")
}

func (instance *countingRefusingConnector) Driver() driver.Driver {
    return nil
}

/* the store is a service whose provider applies the migration set, and a refusal is not remembered by it: the first resolution goes to the database and is refused, and the second goes to the database again, a heal a boot-time build cannot reach. The handle is resolved by name, once, and kept by the container. */
func TestRegisterTwoFactorStoreService_ARefusedMigrationIsAskedAgainAtTheNextResolution(t *testing.T) {
    connector := &countingRefusingConnector{}
    database := bun.NewDB(sql.OpenDB(connector), mysqldialect.New())
    t.Cleanup(func() { _ = database.Close() })

    containerInstance := melodycontainer.NewContainer()

    handleResolutions := 0
    containerInstance.MustRegister(
        serviceDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            handleResolutions++

            return database, nil
        },
        melodycontainer.WithoutTypeRegistration(),
    )

    moduleInstance := &Module{processContext: context.Background(), database: newUndialedDatabase()}
    moduleInstance.registerTwoFactorStoreService(containerRegistrar{Container: containerInstance})

    attemptsBefore := int64(0)
    for resolution := 1; resolution <= 2; resolution++ {
        store, storeErr := melodycontainer.FromResolver[*twofactor.Store](containerInstance, twofactor.ServiceStore)
        if nil == storeErr || nil != store {
            t.Fatalf("resolution %d: expected the refused migration to refuse the store, got %v, %v", resolution, store, storeErr)
        }

        if false == strings.Contains(storeErr.Error(), "migration") {
            t.Fatalf("resolution %d: expected the refusal to be the migration's, got %q", resolution, storeErr.Error())
        }

        attempts := connector.attempts.Load()
        if attempts <= attemptsBefore {
            t.Fatalf("resolution %d: expected the migration to go to the database again, the attempts stayed at %d", resolution, attempts)
        }
        attemptsBefore = attempts
    }

    if 1 != handleResolutions {
        t.Fatalf("expected the handle resolved by name once and kept, got %d resolutions", handleResolutions)
    }
}

/* without a catalogue database the environment has no enrollment table, and the store is not registered: the
   doors and the release stand on the same switch and are not registered either */
func TestRegisterTwoFactorStoreService_RegistersNothingWithoutADatabase(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()

    moduleInstance := &Module{processContext: context.Background()}
    moduleInstance.registerTwoFactorStoreService(containerRegistrar{Container: containerInstance})

    if true == containerInstance.Has(twofactor.ServiceStore) {
        t.Fatal("expected no store registered without a database")
    }
}
