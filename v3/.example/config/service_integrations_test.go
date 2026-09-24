package config

import (
    "database/sql"
    "testing"

    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodylockcontract "github.com/precision-soft/melody/v3/lock/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
)

/* without an archive the locker under the archive's name is the in-process one: the in-process archive then has a producer, where a missing locker made the refresh skip the write on every run and the history door answer an empty list for the life of the process */
func TestRegisterArchiveLockerService_HandsTheInProcessLockerWhenNoArchiveIsWired(t *testing.T) {
    moduleInstance := &Module{}
    serviceContainer := melodycontainer.NewContainer()

    moduleInstance.registerArchiveLockerService(&containerRegistrar{Container: serviceContainer})

    locker, resolveErr := melodycontainer.FromResolver[melodylockcontract.Locker](serviceContainer, persistence.ServiceArchiveLocker)
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
    serviceContainer := melodycontainer.NewContainer()

    handleResolutions := 0
    serviceContainer.MustRegister(
        serviceArchiveDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            handleResolutions++

            return bun.NewDB(sql.OpenDB(&refusingConnector{}), pgdialect.New()), nil
        },
        melodycontainer.WithoutTypeRegistration(),
    )

    moduleInstance := &Module{archiveWired: true}
    moduleInstance.registerArchiveLockerService(&containerRegistrar{Container: serviceContainer})

    if 0 != handleResolutions {
        t.Fatalf("expected registering the locker to open nothing, the handle was resolved %d times", handleResolutions)
    }

    locker, resolveErr := melodycontainer.FromResolver[melodylockcontract.Locker](serviceContainer, persistence.ServiceArchiveLocker)
    if nil != resolveErr {
        t.Fatalf("resolve the archive locker: %v", resolveErr)
    }

    if _, advisory := locker.(*melodypgsql.Locker); false == advisory || 1 != handleResolutions {
        t.Fatalf("expected the archive database's advisory lock over its handle, got %T after %d handle resolutions", locker, handleResolutions)
    }

    if _, byTypeErr := melodycontainer.FromResolverByType[melodylockcontract.Locker](serviceContainer); nil == byTypeErr {
        t.Fatal("expected the archive's locker off the type index")
    }
}
