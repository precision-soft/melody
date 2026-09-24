package config

import (
    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    bun "github.com/uptrace/bun"
)

/* the two-factor store persists a user's TOTP secret and single-use recovery codes (encrypted at rest via bunorm EncryptedString) and exposes an enroll + verify flow over HTTP, exercising the security/totp package and the enrollment/recovery store end-to-end. It is wired only when a database is configured.

   The enrollment table belongs to the example's migration set rather than to the store: the operator's db:* family and this provider create the same table, and the store is left holding only the reads and writes it exists for.

   The store is a service resolved at the first request that needs it, not a value built at boot. Built at boot, a refusal of the migration — a database briefly down, a volume the reset had yet to bring here — left the enroll and verify routes unregistered and the enrollment release unsubscribed for the life of the process, answering 404 after the cause was gone, while every repository beside it healed at its next resolution. Resolved, the routes are registered whenever the catalogue is, a refusal is answered 503 to the request that met it, and the next request resolves again: neither the container nor the migration funnel remembers a refusal. */
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
