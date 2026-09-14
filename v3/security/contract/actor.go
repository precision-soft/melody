package contract

const (
    ActorTypeUser      = "user"
    ActorTypeApiClient = "api-client"
    ActorTypeSystem    = "system"
)

/* Actor identifies the originating principal, which may differ from the authenticated transport principal. Tokens expose it through the optional ActorAware capability. */
type Actor interface {
    Identifier() string

    /* Type reports the kind of originating actor; one of ActorTypeUser, ActorTypeApiClient or ActorTypeSystem. */
    Type() string

    Roles() []string

    Attributes() map[string]string
}

/* ActorAware optionally exposes an originating actor without widening Token. */
type ActorAware interface {
    OnBehalfOf() (Actor, bool)
}

/* ActorImpersonating exposes the accountable impersonator behind a propagated actor. */
type ActorImpersonating interface {
    Impersonator() (Actor, bool)
}

/* ActorData serializes an originating actor in Claims. Impersonator preserves the accountable administrator and roles across services. */
type ActorData struct {
    Identifier   string            `json:"Identifier"`
    Type         string            `json:"Type"`
    Roles        []string          `json:"Roles,omitempty"`
    Attributes   map[string]string `json:"Attributes,omitempty"`
    Impersonator *ActorData        `json:"Impersonator,omitempty"`
}
