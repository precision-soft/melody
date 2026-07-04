package config

import (
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

    provider := melodymysql.NewProvider()

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
