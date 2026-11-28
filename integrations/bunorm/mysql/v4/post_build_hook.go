package mysql

import (
    driver "github.com/go-sql-driver/mysql"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type PostBuildHook func(
    runtimeInstance containercontract.Runtime,
    driverConfig *driver.Config,
) error
