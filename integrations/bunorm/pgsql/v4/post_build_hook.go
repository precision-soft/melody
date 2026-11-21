package pgsql

import (
    "context"

    "github.com/uptrace/bun/driver/pgdriver"
)

type PostBuildHook func(
    ctx context.Context,
    connector *pgdriver.Connector,
) error
