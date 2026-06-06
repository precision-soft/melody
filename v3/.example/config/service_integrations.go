package config

import (
    "os"
    "path/filepath"

    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v3/cache"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodylockcontract "github.com/precision-soft/melody/v3/lock/contract"
    melodystorage "github.com/precision-soft/melody/v3/storage"
    melodystoragecontract "github.com/precision-soft/melody/v3/storage/contract"
)

func (instance *Module) registerStorageService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil != instance.storageClient {
        melodyawss3.RegisterStorageService(registrar, instance.storageClient, instance.storageBucket)

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

func (instance *Module) registerRedisServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.redisClient {
        return
    }

    melodyrueidiscache.RegisterBackendService(registrar, instance.redisClient, "melody-example:")
    melodyrueidis.RegisterTokenStoreService(registrar, instance.redisClient)
}
