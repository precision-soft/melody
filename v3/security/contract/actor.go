package contract

const (
    ActorTypeUser      = "user"
    ActorTypeApiClient = "api-client"
    ActorTypeSystem    = "system"
)

/* Actor is the originating principal that started an action upstream (for example the human user or client that called service A), distinct from the authenticated transport principal that is carrying the request now (service B). It is optional context attached to a Token through the ActorAware interface; an absent actor means the token behaves exactly as before. */
type Actor interface {
    Identifier() string

    /* Type reports the kind of originating actor; one of ActorTypeUser, ActorTypeApiClient or ActorTypeSystem. */
    Type() string

    Roles() []string

    Attributes() map[string]string
}

/* ActorAware is implemented by tokens that can carry an originating actor. Consumers (voters, audit) type-assert a Token to ActorAware rather than the core Token interface being widened, so existing Token implementations keep compiling. */
type ActorAware interface {
    OnBehalfOf() (Actor, bool)
}

/* ActorImpersonating is implemented by an Actor an impersonator is acting behind. Consumers type-assert it (rather than widening Actor) to read the accountable impersonator a propagated originating actor carries, so an impersonation started in one service stays auditable in the next. */
type ActorImpersonating interface {
    Impersonator() (Actor, bool)
}

/* ActorData is the serializable carrier for an originating actor inside Claims. The Actor interface itself does not round-trip through JSON, so transports (JWT claims, the HMAC envelope) encode this struct and it is rebuilt into a concrete Actor when a token is constructed. Impersonator, when set, is the admin acting behind this actor, so an impersonation's accountable principal and its roles propagate across services. */
type ActorData struct {
    Identifier   string            `json:"Identifier"`
    Type         string            `json:"Type"`
    Roles        []string          `json:"Roles,omitempty"`
    Attributes   map[string]string `json:"Attributes,omitempty"`
    Impersonator *ActorData        `json:"Impersonator,omitempty"`
}
