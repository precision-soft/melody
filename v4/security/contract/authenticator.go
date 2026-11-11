package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

/* Authenticator reads the credentials of a request it Supports. Authenticate answers an authenticated token when every check passed; an unauthenticated token, or a typed nil, when the credentials were refused, which raises security.login.failure; an unavailable token (security.NewUnavailableToken) or an error carrying exception.MarkInfrastructureFailure when a store or remote its check needs did not answer, which the firewall answers 503 with Retry-After and raises no event for, the token always, the marked error unless an http status answers outside what the mark wraps; or any other error, refused with its status, 500 for a plain one, which raises security.login.failure only when that status is 401 or 403, a refused credential, so a malformed request or a fault in the authenticator locks nobody out. */
type Authenticator interface {
    Supports(request httpcontract.Request) bool

    Authenticate(request httpcontract.Request) (Token, error)
}
