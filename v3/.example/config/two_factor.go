package config

import (
    "context"

    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodylogging "github.com/precision-soft/melody/v3/logging"
)

/* the two-factor store persists a user's TOTP secret and single-use recovery codes (encrypted at rest via bunorm EncryptedString) and exposes an enroll + verify flow over HTTP, exercising the security/totp package and the enrollment/recovery store end-to-end. It is wired only when a database is configured.

   The enrollment table belongs to the example's migration set rather than to the store: the operator's db:* family and this build step then create the same table, and the store is left holding only the reads and writes it exists for. */
func (instance *Module) buildTwoFactor() {
    if nil == instance.database {
        return
    }

    if migrateErr := migration.EnsureMigrated(context.Background(), instance.database); nil != migrateErr {
        /* a migration failure (for example the database is briefly unavailable at boot) leaves the feature unwired rather than aborting the whole application — but not silently: the emergency logger is the one channel of this pre-logger window, and without the record the /twofactor routes simply vanished for the life of the process with nothing anywhere saying why */
        melodylogging.EmergencyLogger().Error(
            "two-factor store migration failed; the enroll and verify routes stay unwired",
            map[string]any{"error": migrateErr.Error()},
        )

        return
    }

    instance.twoFactorStore = twofactor.NewStore(instance.database)
}
