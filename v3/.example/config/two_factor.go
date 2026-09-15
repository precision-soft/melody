package config

import (
    "context"

    "github.com/precision-soft/melody/v3/.example/migration"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodylogging "github.com/precision-soft/melody/v3/logging"
)

func (instance *Module) buildTwoFactor() {
    if nil == instance.database {
        return
    }

    if migrateErr := migration.EnsureMigrated(context.Background(), instance.database); nil != migrateErr {

        melodylogging.EmergencyLogger().Error(
            "two-factor store migration failed; the enroll and verify routes stay unwired",
            map[string]any{"error": migrateErr.Error()},
        )

        return
    }

    instance.twoFactorStore = twofactor.NewStore(instance.database)
}
