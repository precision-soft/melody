package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/**
 * TokenEnricher resolves the final claims after a token's signature has been validated. It is a
 * generic hook: an application implements it to turn token Scope into concrete roles/attributes
 * (for example by looking the subject up in a database), keeping any tenant- or product-specific
 * logic out of the security library. It runs only for successfully validated tokens.
 */
type TokenEnricher interface {
    Enrich(runtimeInstance runtimecontract.Runtime, claims Claims) (Claims, error)
}
