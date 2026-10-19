package contract

const (
    ActorTypeUser      = "user"
    ActorTypeApiClient = "api-client"
    ActorTypeSystem    = "system"
)

/* Actor is the originating principal that started an action upstream, distinct from the authenticated transport principal carrying the request now. It is optional context attached to a Token through ActorAware. */
type Actor interface {
    Identifier() string

    /* Type reports the kind of originating actor; one of ActorTypeUser, ActorTypeApiClient or ActorTypeSystem. */
    Type() string

    Roles() []string

    Attributes() map[string]string
}

/* ActorAware is implemented by tokens that can carry an originating actor; consumers type-assert a Token to it. */
type ActorAware interface {
    OnBehalfOf() (Actor, bool)
}

/* ActorImpersonating is implemented by an Actor an impersonator is acting behind, so an impersonation started in one service stays auditable in the next. */
type ActorImpersonating interface {
    Impersonator() (Actor, bool)
}

/* ActorData is the serializable carrier for an originating actor inside Claims, rebuilt into a concrete Actor when a token is constructed. Impersonator, when set, is the admin acting behind this actor. */
type ActorData struct {
    Identifier   string            `json:"Identifier"`
    Type         string            `json:"Type"`
    Roles        []string          `json:"Roles,omitempty"`
    Attributes   map[string]string `json:"Attributes,omitempty"`
    Impersonator *ActorData        `json:"Impersonator,omitempty"`
}
