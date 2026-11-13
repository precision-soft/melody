package contract

import (
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

type TokenResolver func(request httpcontract.Request) Token
