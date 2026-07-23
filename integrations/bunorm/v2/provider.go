package bunorm

import (
    "github.com/uptrace/bun"

    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type Provider interface {
    Open(params ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error)
}
