package config

import (
    "os"
    "path/filepath"

    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/.example/persistence"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodylockcontract "github.com/precision-soft/melody/v3/lock/contract"
    melodystorage "github.com/precision-soft/melody/v3/storage"
    melodystoragecontract "github.com/precision-soft/melody/v3/storage/contract"
    bun "github.com/uptrace/bun"
)

func (instance *Module) registerStorageService(registrar melodyapplicationcontract.ServiceRegistrar) {

    if nil != instance.storageClient {
        return
    }

    registrar.RegisterService(
        melodystorage.ServiceStorage,
        func(resolver melodycontainercontract.Resolver) (melodystoragecontract.Storage, error) {
            return melodystorage.NewLocalStorage(filepath.Join(os.TempDir(), "melody-example-storage")), nil
        },
    )
}

func (instance *Module) registerLockerService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil != instance.redisClient {
        melodyrueidis.RegisterLockerService(registrar, instance.redisClient)

        return
    }

    if nil != instance.database {
        melodymysql.RegisterLockerService(registrar, instance.database)

        return
    }

    registrar.RegisterService(
        melodylock.ServiceLocker,
        func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            return melodylock.NewInMemoryLocker(melodyclock.NewSystemClock()), nil
        },
    )
}

func (instance *Module) registerArchiveLockerService(registrar melodyapplicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        persistence.ServiceArchiveLocker,
        func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            if false == instance.archiveWired {
                return melodylock.NewInMemoryLocker(melodyclock.NewSystemClock()), nil
            }

            database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver, serviceArchiveDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            return melodypgsql.NewLocker(database), nil
        },

        melodycontainer.WithoutTypeRegistration(),
    )
}
