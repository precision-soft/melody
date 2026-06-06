package config

import (
    "os"

    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v3"
    melodymysql "github.com/precision-soft/melody/integrations/bunorm/mysql/v3"
    "github.com/precision-soft/melody/v3/exception"
)

func (instance *Module) buildDatabase() {
    host := os.Getenv("MYSQL_HOST")
    if "" == host {
        return
    }

    port := os.Getenv("MYSQL_PORT")
    if "" == port {
        port = "3306"
    }

    provider := melodymysql.NewProvider()

    database, openErr := provider.Open(
        melodybunorm.ConnectionParams{
            Host:     host,
            Port:     port,
            Database: os.Getenv("MYSQL_DATABASE"),
            User:     os.Getenv("MYSQL_USER"),
            Password: os.Getenv("MYSQL_PASSWORD"),
        },
        nil,
    )
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }

    instance.database = database
}
