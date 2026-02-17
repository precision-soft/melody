package bunorm

import (
	"github.com/uptrace/bun"

	loggingcontract "github.com/precision-soft/melody/logging/contract"
)

type Provider interface {
	Open(params ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)
}
