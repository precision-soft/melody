package contract

import (
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type TokenEnricher interface {
    Enrich(runtimeInstance runtimecontract.Runtime, claims Claims) (Claims, error)
}
