package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type TokenEnricher interface {
    Enrich(runtimeInstance runtimecontract.Runtime, claims Claims) (Claims, error)
}
