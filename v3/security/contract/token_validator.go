package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Claims struct {
    UserIdentifier string
    Roles          []string

    /**
     * Scope carries generic, application-defined claim data extracted from the token (e.g. a JSON
     * object claim). A TokenEnricher reads it to resolve the final roles/attributes after the
     * signature has been validated. The library assigns no meaning to its keys.
     */
    Scope map[string]any

    /** Attributes holds generic extra data an enricher may attach for downstream use. */
    Attributes map[string]any
}

type TokenValidator interface {
    Validate(runtimeInstance runtimecontract.Runtime, tokenString string) (Claims, error)
}
