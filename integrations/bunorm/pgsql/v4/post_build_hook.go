package pgsql

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/uptrace/bun/driver/pgdriver"
)

type PostBuildHook func(
    runtimeInstance containercontract.Runtime,
    connector *pgdriver.Connector,
) error
