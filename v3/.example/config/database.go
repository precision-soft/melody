package config

import (
    "time"

    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"
    "github.com/precision-soft/melody/v3/exception"
)

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
    provider := melodymysql.NewProvider(
        melodymysql.WithRetryConfig(melodymysql.NewRetryConfig(10, time.Second, 5*time.Second, 2.0)),
    )

    database, openErr := provider.Open(
        melodybunorm.ConnectionParams{
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
