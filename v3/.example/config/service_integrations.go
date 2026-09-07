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
    /* the S3 backend is registered by the awss3 module when configured (see configure.go); this is the local-disk fallback. */
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

/* registerArchiveLockerService publishes the archive's locker, and only when an archive is wired: without one there is nothing to serialise and nothing to take a lock on, and the writer that would have used it is not registered either. It is resolved LAZILY through the archive handle's own service, so registering it costs no connection. */
func (instance *Module) registerArchiveLockerService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if false == instance.archiveWired {
        return
    }

    registrar.RegisterService(
        persistence.ServiceArchiveLocker,
        func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver, serviceArchiveDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            return melodypgsql.NewLocker(database), nil
        },
        /* the framework's ServiceLocker registration above already claims the Locker contract on the type index, and this is a second implementation of it — asked for by name, because "the locker" of this application is that one and this is the archive's */
        melodycontainer.WithoutTypeRegistration(),
    )
}
