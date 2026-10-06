package config

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "os"
    "strings"
    "sync/atomic"
    "testing"

    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/redis/rueidis"
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

/* the store is a service whose provider applies the migration set, and a dial refusal is not remembered by it (only a lock wait that ran out its window is, see EnsureMigrated): the first resolution goes to the database and is refused, and the second goes to the database again, a heal a boot-time build cannot reach. The handle is resolved by name, once, and kept by the container. */
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

/* budgetFailureLogger keeps the error records the budget's limiter writes; every other level falls to the embedded nop logger */
type budgetFailureLogger struct {
    melodyloggingcontract.Logger
    messageList []string
}

func (instance *budgetFailureLogger) Error(message string, context melodyloggingcontract.Context) {
    instance.messageList = append(instance.messageList, message)
}

/* a failed release of the budget carries no request, so the redis limiter records it on the application's logger the module hands it, inside the configured journal rather than on standard error */
func TestBuildSecondFactorBudgetLimiter_RecordsAFailedReleaseInTheConfiguredJournal(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping the redis second factor budget test")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{InitAddress: []string{address}})
    if nil != clientErr {
        t.Fatalf("could not connect to redis: %v", clientErr)
    }
    client.Close()

    logger := &budgetFailureLogger{Logger: melodylogging.NewNopLogger()}

    limiter := (&Module{redisClient: client}).buildSecondFactorBudgetLimiter(melodyclock.NewSystemClock(), logger)
    limiter.Reset("account:user-2")

    if 1 != len(logger.messageList) || "rate limiter store failure" != logger.messageList[0] {
        t.Fatalf("expected the failed release recorded once in the configured journal, got %v", logger.messageList)
    }
}
