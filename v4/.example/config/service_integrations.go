package config

import (
    "os"
    "path/filepath"
    "strings"

    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v4"
    melodypgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v4"
    "github.com/precision-soft/melody/v4/.example/persistence"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
    melodylockcontract "github.com/precision-soft/melody/v4/lock/contract"
    melodystorage "github.com/precision-soft/melody/v4/storage"
    melodystoragecontract "github.com/precision-soft/melody/v4/storage/contract"
    bun "github.com/uptrace/bun"
)

func (instance *Module) registerStorageService(registrar melodyapplicationcontract.ServiceRegistrar) {
    /* the S3 backend is registered by the aws.s3 module when configured (see configure.go); this is the local-disk fallback. */
    if nil != instance.storageClient {
        return
    }

    registrar.RegisterService(
        melodystorage.ServiceStorage,
        func(resolver melodycontainercontract.Resolver) (melodystoragecontract.Storage, error) {
            return melodystorage.NewLocalStorage(instance.localStorageDirectory(), melodystorage.LocalStorageConfig{}), nil
        },
        melodycontainer.RegisterOptions{},
    )
}

/* localStorageDirectory is APP_LOCAL_STORAGE_DIRECTORY, a relative value anchored to the project directory as APP_SESSION_FILE is, so a console run and the http process write one directory; the system's temporary directory under melody-example-storage when the key is empty */
func (instance *Module) localStorageDirectory() string {
    if directory := strings.TrimSpace(instance.parameterValue(parameterLocalStorageDirectory)); "" != directory {
        if false == filepath.IsAbs(directory) {
            return filepath.Join(instance.configuration.Kernel().ProjectDirectory(), directory)
        }

        return directory
    }

    return filepath.Join(os.TempDir(), "melody-example-storage")
}

func (instance *Module) registerLockerService(registrar melodyapplicationcontract.ServiceRegistrar) {
    /* the rueidis module registers the redis locker over the connection */
    if true == instance.redisConfigured() {
        return
    }

    /* the catalogue's advisory lock, its handle resolved at the locker's first use rather than captured at registration, so the handle is opened by its own service and closed with it */
    if true == instance.catalogueWired {
        registrar.RegisterService(
            melodylock.ServiceLocker,
            func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
                database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver.Runtime().Context(), resolver, serviceDatabase)
                if nil != resolveErr {
                    return nil, resolveErr
                }

                return melodymysql.NewLocker(database, melodymysql.LockerConfig{}), nil
            },
            melodycontainer.RegisterOptions{},
        )

        return
    }

    registrar.RegisterService(
        melodylock.ServiceLocker,
        func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            clockInstance, clockErr := melodycontainer.FromResolver[melodyclockcontract.Clock](resolver.Runtime().Context(), resolver, melodyclock.ServiceClock)
            if nil != clockErr {
                return nil, clockErr
            }

            return melodylock.NewInMemoryLocker(melodylock.InMemoryLockerConfig{Clock: clockInstance}), nil
        },
        melodycontainer.RegisterOptions{},
    )
}

/* registerArchiveLockerService publishes the archive's locker: with an archive wired, the advisory lock of the archive's database, resolved lazily through the archive handle's service; without one, the in-process locker, which is the whole exclusion an in-process archive needs. Without postgres the refresh records into its own process's archive, and the http server's history door answers an empty list. */
func (instance *Module) registerArchiveLockerService(registrar melodyapplicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        persistence.ServiceArchiveLocker,
        func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            if false == instance.archiveWired {
                clockInstance, clockErr := melodycontainer.FromResolver[melodyclockcontract.Clock](resolver.Runtime().Context(), resolver, melodyclock.ServiceClock)
                if nil != clockErr {
                    return nil, clockErr
                }

                return melodylock.NewInMemoryLocker(melodylock.InMemoryLockerConfig{Clock: clockInstance}), nil
            }

            database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver.Runtime().Context(), resolver, serviceArchiveDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            return melodypgsql.NewLocker(database, melodypgsql.LockerConfig{}), nil
        },
        /* the framework's ServiceLocker registration above already claims the Locker contract on the type index, and this is a second implementation of it — asked for by name, because "the locker" of this application is that one and this is the archive's */
        melodycontainer.RegisterOptions{TypeRegistrationDisabled: true},
    )
}
