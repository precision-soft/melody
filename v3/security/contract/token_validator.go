package contract

import (
    "time"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Claims struct {
    UserIdentifier string   `json:"UserIdentifier"`
    Roles          []string `json:"Roles"`

    IssuedAt         time.Time `json:"IssuedAt,omitzero"`
    DeviceIdentifier string    `json:"DeviceIdentifier,omitempty"`

    Scope map[string]any `json:"Scope,omitempty"`

    Attributes map[string]any `json:"Attributes,omitempty"`

    OriginatingActor *ActorData `json:"OriginatingActor,omitempty"`
}

type TokenValidator interface {
    Validate(runtimeInstance runtimecontract.Runtime, tokenString string) (Claims, error)
}
