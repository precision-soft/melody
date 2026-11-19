package config

import (
    "context"
    "database/sql"
    "os"
    "path/filepath"
    "testing"
    "time"

    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v4"
    "github.com/precision-soft/melody/v4/.example/persistence"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
    melodylockcontract "github.com/precision-soft/melody/v4/lock/contract"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
)

func TestRegisterArchiveLockerService_HandsTheInProcessLockerWhenNoArchiveIsWired(t *testing.T) {
    moduleInstance := &Module{}
    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister[melodyclockcontract.Clock](serviceContainer, melodyclock.ServiceClock, func(melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return melodyclock.NewSystemClock(), nil
    }, melodycontainer.RegisterOptions{})

    moduleInstance.registerArchiveLockerService(&containerRegistrar{Container: serviceContainer})

    locker, resolveErr := melodycontainer.FromResolver[melodylockcontract.Locker](t.Context(), serviceContainer, persistence.ServiceArchiveLocker)
    if nil != resolveErr {
        t.Fatalf("expected the archive locker to be registered without an archive, got %v", resolveErr)
    }

    if _, inProcess := locker.(*melodylock.InMemoryLocker); false == inProcess {
        t.Fatalf("expected the in-process locker without an archive, got %T", locker)
    }
}

func TestRegisterArchiveLockerService_HandsTheArchiveDatabasesAdvisoryLockWhenTheArchiveIsWired(t *testing.T) {
    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    handleResolutions := 0
    serviceContainer.MustRegister(
        serviceArchiveDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            handleResolutions++

            return bun.NewDB(sql.OpenDB(&refusingConnector{}), pgdialect.New()), nil
        },
        melodycontainer.RegisterOptions{TypeRegistrationDisabled: true, Closer: registryOwnedDatabaseCloser},
    )

    moduleInstance := &Module{archiveWired: true}
    moduleInstance.registerArchiveLockerService(&containerRegistrar{Container: serviceContainer})

    if 0 != handleResolutions {
        t.Fatalf("expected registering the locker to open nothing, the handle was resolved %d times", handleResolutions)
    }

    locker, resolveErr := melodycontainer.FromResolver[melodylockcontract.Locker](t.Context(), serviceContainer, persistence.ServiceArchiveLocker)
    if nil != resolveErr {
        t.Fatalf("resolve the archive locker: %v", resolveErr)
    }

    if _, advisory := locker.(*melodypgsql.Locker); false == advisory || 1 != handleResolutions {
        t.Fatalf("expected the archive database's advisory lock over its handle, got %T after %d handle resolutions", locker, handleResolutions)
    }

    if _, byTypeErr := melodycontainer.FromResolverByType[melodylockcontract.Locker](t.Context(), serviceContainer); nil == byTypeErr {
        t.Fatal("expected the archive's locker off the type index")
    }
}

func TestLocalStorageDirectory_ReadsItsKey(t *testing.T) {
    for _, testCase := range []struct {
        value    string
        expected string
    }{
        {value: "/srv/objects", expected: "/srv/objects"},
        {value: " var/storage ", expected: "/tmp/melody/var/storage"},
        {value: "", expected: filepath.Join(os.TempDir(), "melody-example-storage")},
    } {
        environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), &stubEnvironmentSource{values: map[string]string{"APP_LOCAL_STORAGE_DIRECTORY": testCase.value}})
        if nil != environmentErr {
            t.Fatalf("new environment: %v", environmentErr)
        }

        configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody", melodyconfig.ConfigurationConfig{EnvironmentKeys: EnvironmentKeys()})
        if nil != configurationErr {
            t.Fatalf("new configuration: %v", configurationErr)
        }

        if directory := (&Module{configuration: configuration}).localStorageDirectory(); testCase.expected != directory {
            t.Fatalf("%q: expected %q, got %q", testCase.value, testCase.expected, directory)
        }
    }
}

func assertInProcessLockerExpiresOnTheContainersClock(t *testing.T, register func(*Module, melodyapplicationcontract.ServiceRegistrar), serviceName string) {
    t.Helper()

    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC))
    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = serviceContainer.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister[melodyclockcontract.Clock](serviceContainer, melodyclock.ServiceClock, func(melodycontainercontract.Resolver) (melodyclockcontract.Clock, error) {
        return clockInstance, nil
    }, melodycontainer.RegisterOptions{})

    register(&Module{}, &containerRegistrar{Container: serviceContainer})

    locker, resolveErr := melodycontainer.FromResolver[melodylockcontract.Locker](t.Context(), serviceContainer, serviceName)
    if nil != resolveErr {
        t.Fatalf("resolve %s: %v", serviceName, resolveErr)
    }

    runtimeInstance := melodyruntime.New(t.Context(), serviceContainer.NewScope(), serviceContainer)

    if acquired, acquireErr := locker.CreateLock("example-lock", melodybound.Of(time.Minute)).Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the first acquire to hold the lock, got %t, %v", acquired, acquireErr)
    }

    second := locker.CreateLock("example-lock", melodybound.Of(time.Minute))
    if acquired, acquireErr := second.Acquire(runtimeInstance); nil != acquireErr || true == acquired {
        t.Fatalf("expected the second acquire refused while the lock is held, got %t, %v", acquired, acquireErr)
    }

    clockInstance.Advance(61 * time.Second)

    if acquired, acquireErr := second.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the lock lapsed on the container's clock, got %t, %v", acquired, acquireErr)
    }
}

func assertInProcessLockerRefusesWithoutAClock(t *testing.T, register func(*Module, melodyapplicationcontract.ServiceRegistrar), serviceName string) {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = serviceContainer.CloseWithContext(context.Background()) })

    register(&Module{}, &containerRegistrar{Container: serviceContainer})

    if _, resolveErr := melodycontainer.FromResolver[melodylockcontract.Locker](t.Context(), serviceContainer, serviceName); nil == resolveErr {
        t.Fatalf("expected %s refused without a clock", serviceName)
    }
}

func TestRegisterLockerService_InProcessLockerExpiresOnTheContainersClock(t *testing.T) {
    assertInProcessLockerExpiresOnTheContainersClock(t, (*Module).registerLockerService, melodylock.ServiceLocker)
}

func TestRegisterArchiveLockerService_InProcessLockerExpiresOnTheContainersClock(t *testing.T) {
    assertInProcessLockerExpiresOnTheContainersClock(t, (*Module).registerArchiveLockerService, persistence.ServiceArchiveLocker)
}

func TestRegisterLockerService_RefusesWithoutAClock(t *testing.T) {
    assertInProcessLockerRefusesWithoutAClock(t, (*Module).registerLockerService, melodylock.ServiceLocker)
}

func TestRegisterArchiveLockerService_RefusesWithoutAClock(t *testing.T) {
    assertInProcessLockerRefusesWithoutAClock(t, (*Module).registerArchiveLockerService, persistence.ServiceArchiveLocker)
}
