package mysql

import (
    "context"

    driver "github.com/go-sql-driver/mysql"
)

type PostBuildHook func(
    ctx context.Context,
    driverConfig *driver.Config,
) error
