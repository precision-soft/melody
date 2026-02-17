package mysql

import (
	"context"

	driver "github.com/go-sql-driver/mysql"
	containercontract "github.com/precision-soft/melody/container/contract"
)

type PostBuildHook func(
	ctx context.Context,
	resolver containercontract.Resolver,
	driverConfig *driver.Config,
) error
