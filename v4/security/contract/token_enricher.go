package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type TokenEnricher interface {
    Enrich(runtimeInstance containercontract.Runtime, claims Claims) (Claims, error)
}
