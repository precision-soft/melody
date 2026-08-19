package config

import (
    "time"

    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    bun "github.com/uptrace/bun"
)

/* serviceDatabase is the container name of the example's shared *bun.DB; the outbox store factory and the
encrypt database factory resolve it at their first use instead of receiving the prebuilt handle at
composition-root time. */
const serviceDatabase = "service-example-database"

/*
dialIsInsecure reads a transport switch. The provider negotiates a verified
TLS handshake by default; the development compose mysql speaks plain TCP, so
the shipped .env arms the insecure dial explicitly — the decision is visible
in configuration rather than buried in the wiring. The spelling is exact: any
value but "true" keeps the verified handshake, because a credential-bearing
dial downgrades only on an unambiguous instruction.
*/
func dialIsInsecure(insecureValue string) bool {
    return "true" == insecureValue
}

func (instance *Module) buildDatabase() {
    host := instance.environmentValue(environmentKeyMysqlHost)
    if "" == host {
        return
    }

    port := instance.environmentValue(environmentKeyMysqlPort)
    if "" == port {
        port = "3306"
    }

    /* retry the initial connection with backoff so the example survives a cold-start race against the
       database container — mysql often takes 20-30s to accept connections while the app boots in seconds,
       and buildDatabase panics on a hard failure. The provider only retries transient errors (connection
       refused), so a real misconfiguration still fails fast. */
    optionList := []melodymysql.ProviderOption{
        melodymysql.WithRetryConfig(melodymysql.NewRetryConfig(10, time.Second, 5*time.Second, 2.0)),
    }
    if true == dialIsInsecure(instance.environmentValue(environmentKeyMysqlInsecure)) {
        optionList = append(optionList, melodymysql.WithInsecure(true))
    }

    provider := melodymysql.NewProvider(optionList...)

    database, openErr := provider.Open(
        melodybunorm.ConnectionParameters{
            Host:     host,
            Port:     port,
            Database: instance.environmentValue(environmentKeyMysqlDatabase),
            User:     instance.environmentValue(environmentKeyMysqlUser),
            Password: instance.environmentValue(environmentKeyMysqlPassword),
        },
        nil,
    )
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }

    instance.database = database
}

/* registerDatabaseService publishes the eagerly-opened *bun.DB under serviceDatabase so the lazy factories
(outbox store, encrypt bulk command) can resolve it from the container; without a configured database the
service stays unregistered and a factory's first use reports the missing service instead of failing boot. */
func (instance *Module) registerDatabaseService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.database {
        return
    }

    registrar.RegisterService(
        serviceDatabase,
        func(resolver melodycontainercontract.Resolver) (*bun.DB, error) {
            return instance.database, nil
        },
    )
}
