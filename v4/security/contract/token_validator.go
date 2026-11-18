package contract

import (
    "time"

    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type Claims struct {
    UserIdentifier string   `json:"userIdentifier"`
    Roles          []string `json:"roles"`

    IssuedAt         time.Time `json:"issuedAt,omitzero"`
    DeviceIdentifier string    `json:"deviceIdentifier,omitempty"`

    Scope map[string]any `json:"scope,omitempty"`

    Attributes map[string]any `json:"attributes,omitempty"`

    OriginatingActor *ActorData `json:"originatingActor,omitempty"`
}

type TokenValidator interface {
    Validate(runtimeInstance runtimecontract.Runtime, tokenString string) (Claims, error)
}
