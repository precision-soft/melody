package pgsql

import (
	"context"

	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/uptrace/bun/driver/pgdriver"
)

type PostBuildHook func(
	ctx context.Context,
	resolver containercontract.Resolver,
	connector *pgdriver.Connector,
) error
