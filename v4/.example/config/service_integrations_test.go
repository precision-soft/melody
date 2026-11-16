package config

import (
    "database/sql"
    "os"
    "path/filepath"
    "testing"

    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v4"
    "github.com/precision-soft/melody/v4/.example/persistence"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
    melodylockcontract "github.com/precision-soft/melody/v4/lock/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
)

/* without an archive the locker under the archive's name is the in-process one, so the in-process archive has a producer; without a locker the refresh would skip the write on every run */
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

/* with an archive the locker under the archive's name is the advisory lock of the archive's OWN database,
   reached through the archive handle's service — so the exclusion lives where the archive does, and registering
   it opens nothing. It is kept off the type index, where the application's general locker already claims the
   contract: "the locker" is that one, and this is asked for by name. */
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

/* the local storage directory is APP_LOCAL_STORAGE_DIRECTORY, the temporary directory when the key is empty */
func TestLocalStorageDirectory_ReadsItsKey(t *testing.T) {
    for _, testCase := range []struct {
        value    string
        expected string
    }{
        {value: "/srv/objects", expected: "/srv/objects"},
        {value: " var/storage ", expected: "/tmp/melody/var/storage"},
        {value: "", expected: filepath.Join(os.TempDir(), "melody-example-storage")},
    } {
        environment, environmentErr := melodyconfig.NewEnvironment(&stubEnvironmentSource{values: map[string]string{"APP_LOCAL_STORAGE_DIRECTORY": testCase.value}})
        if nil != environmentErr {
            t.Fatalf("new environment: %v", environmentErr)
        }

        configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody")
        if nil != configurationErr {
            t.Fatalf("new configuration: %v", configurationErr)
        }

        if directory := (&Module{configuration: configuration}).localStorageDirectory(); testCase.expected != directory {
            t.Fatalf("%q: expected %q, got %q", testCase.value, testCase.expected, directory)
        }
    }
}
