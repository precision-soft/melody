package bunorm

import (
    "github.com/uptrace/bun"

    containercontract "github.com/precision-soft/melody/container/contract"
)

type Provider interface {
    Open(resolver containercontract.Resolver) (*bun.DB, error)
}
