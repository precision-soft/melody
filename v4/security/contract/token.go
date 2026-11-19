package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type Token interface {
    IsAuthenticated() bool

    UserIdentifier() string

    Roles() []string

    Scope() map[string]any

    Attributes() map[string]any
}

/* AuthenticationUnavailable is implemented by the token a source or an Authenticator answers when a credential was presented and the store or remote its check stands on could not answer. The token is not authenticated: on a route that requires authentication the firewall answers 503 with Retry-After instead of its entry point, on a public route the request is served as anonymous. */
type AuthenticationUnavailable interface {
    AuthenticationUnavailableCause() error
}

/* TokenResolver answers the request's token through the runtime it is handed; an error is answered as a token source's, refused with its status, 500 for a plain one and the unavailable reading for one carrying exception.MarkInfrastructureFailure, and a typed nil error is no error. */
type TokenResolver func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (Token, error)
