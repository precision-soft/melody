package repository

import (
    "github.com/precision-soft/melody/v4/.example/migration"
    "github.com/precision-soft/melody/v4/.example/persistence"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* PrepareCatalog makes the catalogue ready under the runtime it is handed: the migration set, then the audit trail's tables, then the seeds in the order the rows refer to each other, category and currency before the product, the accounts last. A storage with no database is ready as it is. Every step is safe to run again: a success of the migration set is remembered, the trail's tables are created only when absent and a seed writes only into an empty table, so a run interrupted by its caller leaves what the next run finishes. */
func PrepareCatalog(runtimeInstance melodycontainercontract.Runtime, storage *persistence.CatalogStorage) error {
    if refuseErr := melodycontainercontract.RefuseNilRuntime(runtimeInstance, "repository.PrepareCatalog"); nil != refuseErr {
        return refuseErr
    }

    if false == storage.IsPersistent() {
        return nil
    }

    if migrateErr := migration.EnsureMigrated(runtimeInstance.Context(), storage.Database()); nil != migrateErr {
        return migrateErr
    }

    if schemaErr := storage.EnsureAuditSchema(runtimeInstance); nil != schemaErr {
        return schemaErr
    }

    for _, seeder := range []Seeder{newCategorySeeder(storage), newCurrencySeeder(storage), newProductSeeder(storage), newUserSeeder(storage)} {
        if seedErr := seeder.SeedIfEmpty(runtimeInstance.Context()); nil != seedErr {
            return seedErr
        }
    }

    return nil
}
