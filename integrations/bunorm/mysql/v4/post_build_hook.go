package mysql

import (
    driver "github.com/go-sql-driver/mysql"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* PostBuildHook configures the driver during the open, handed the runtime the open runs under over a context ending at the connect timeout; the runtime is whichever caller's opened the pool, the boot's, a command's or the first request's, and its context ends when the open returns: the hook resolves through the runtime's scope what it needs and keeps the services, never the runtime, and reads nothing scoped to a request. */
type PostBuildHook func(
    runtimeInstance containercontract.Runtime,
    driverConfig *driver.Config,
) error
