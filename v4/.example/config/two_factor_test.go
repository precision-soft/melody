package config

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "os"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    "github.com/precision-soft/melody/v4/.example/twofactor"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    "github.com/redis/rueidis"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

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

func TestRegisterTwoFactorStoreService_ARefusedMigrationIsAskedAgainAtTheNextResolution(t *testing.T) {
    connector := &countingRefusingConnector{}
    database := bun.NewDB(sql.OpenDB(connector), mysqldialect.New())
    t.Cleanup(func() { _ = database.Close() })

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    handleResolutions := 0
    containerInstance.MustRegister(
        serviceDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            handleResolutions++

            return database, nil
        },
        melodycontainer.RegisterOptions{TypeRegistrationDisabled: true, Closer: registryOwnedDatabaseCloser},
    )

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.catalogueWired = true
    moduleInstance.registerCatalogStorageService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerCatalogSchemaService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerTwoFactorStoreService(containerRegistrar{Container: containerInstance})

    attemptsBefore := int64(0)
    for resolution := 1; resolution <= 2; resolution++ {
        store, storeErr := melodycontainer.FromResolver[*twofactor.Store](t.Context(), containerInstance, twofactor.ServiceStore)
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

func TestRegisterTwoFactorStoreService_RegistersNothingWithoutADatabase(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    moduleInstance := &Module{processContext: context.Background()}
    moduleInstance.registerTwoFactorStoreService(containerRegistrar{Container: containerInstance})

    if true == containerInstance.Has(twofactor.ServiceStore) {
        t.Fatal("expected no store registered without a database")
    }
}

func TestBuildSecondFactorBudgetLimiter_AFailedReleaseIsReturnedMarked(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping the redis second factor budget test")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{InitAddress: []string{address}})
    if nil != clientErr {
        t.Fatalf("could not connect to redis: %v", clientErr)
    }
    client.Close()

    moduleInstance := &Module{redisProvider: melodyrueidis.NewProvider("melody-example-v4", melodyrueidis.ProviderConfig{})}

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        melodyrueidis.ServiceConnection,
        func(resolver melodycontainercontract.Resolver) (*melodyrueidis.Connection, error) {
            return melodyrueidis.NewConnection(client, "melody-example-v4"), nil
        },
        melodycontainer.RegisterOptions{},
    )
    moduleInstance.registerRedisServices(containerRegistrar{Container: containerInstance})

    limiter := moduleInstance.buildSecondFactorBudgetLimiter(melodyclock.NewSystemClock())

    if resetErr := limiter.Reset(melodyruntime.NewRun(t.Context(), containerInstance.NewScope()), "account:user-2"); false == melodyexception.AnswersUnavailable(resetErr) {
        t.Fatalf("expected the failed release returned marked, got %v", resetErr)
    }
}

func TestBuildSecondFactorBudgetLimiter_GivesTheSlotOfTheAccountSeenLongestAgoPastItsCeiling(t *testing.T) {
    limiter := (&Module{}).buildSecondFactorBudgetLimiter(melodyclock.NewFrozenClock(time.Unix(1700000000, 0)))

    for account := 0; account < inProcessWriteThrottleMaxAddresses; account++ {
        for attempt := 0; attempt < secondFactorBudgetAllowance; attempt++ {
            if false == allowedBy(limiter, fmt.Sprintf("account-%d", account)) {
                t.Fatalf("expected account %d attempt %d admitted below the ceiling", account, attempt)
            }
        }
    }

    if true == allowedBy(limiter, "account-1") {
        t.Fatal("expected a tracked account to keep its spent budget")
    }

    if false == allowedBy(limiter, "account-past-the-ceiling") {
        t.Fatalf("expected an unseen account admitted once %d accounts are tracked", inProcessWriteThrottleMaxAddresses)
    }

    if false == allowedBy(limiter, "account-0") {
        t.Fatal("expected the account seen longest ago evicted and back with a full budget")
    }
}
