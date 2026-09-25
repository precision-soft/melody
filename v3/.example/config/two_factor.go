package config

import (
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    bun "github.com/uptrace/bun"
)

/* registerTwoFactorStoreService wires the two-factor store, which keeps a user's TOTP secret and single-use recovery codes encrypted at rest, only when a database is configured; the enrollment table belongs to the example's migration set. The store is resolved at the first request that needs it, so a refused migration answers 503 to that request and the next one resolves again. */
func (instance *Module) registerTwoFactorStoreService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.database {
        return
    }

    registrar.RegisterService(
        twofactor.ServiceStore,
        func(resolver melodycontainercontract.Resolver) (*twofactor.Store, error) {
            /* the handle is resolved by name, the way the catalogue storage resolves it, so the store closes ahead of the pool it reads through */
            database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver, serviceDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            if migrateErr := migration.EnsureMigrated(instance.processContext, database); nil != migrateErr {
                return nil, migrateErr
            }

            return twofactor.NewStore(database), nil
        },
    )
}
