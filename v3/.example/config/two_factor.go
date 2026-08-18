package config

import (
    "context"

    "github.com/precision-soft/melody/v3/.example/twofactor"
)

/* the two-factor store persists a user's TOTP secret and single-use recovery codes (encrypted at rest via
bunorm EncryptedString) and exposes an enroll + verify flow over HTTP, exercising the security/totp package
and the enrollment/recovery store end-to-end. It is wired only when a database is configured. */
func (instance *Module) buildTwoFactor() {
    if nil == instance.database {
        return
    }

    store := twofactor.NewStore(instance.database)
    if schemaErr := store.EnsureSchema(context.Background()); nil != schemaErr {
        /* a table-creation failure (for example the database is briefly unavailable at boot) leaves
           the feature unwired rather than aborting the whole application */
        return
    }

    instance.twoFactorStore = store
}
