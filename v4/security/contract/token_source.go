package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* TokenSource reads the caller of a request. Resolve answers an authenticated token when every check ran and passed; an anonymous token when no credential was presented or the one presented failed a check; an unavailable token (security.NewUnavailableToken) when a credential was presented and a store or remote its check needs did not answer, never anonymous for it, since the firewall would then tell a caller holding a good credential to sign in again; or an error, which refuses the request on every route with the error's status, 500 for a plain one. An error carrying exception.MarkInfrastructureFailure is read as the unavailable token, unless an http status answers outside what the mark wraps, which is kept. A source that decorates another passes an unavailable token through untouched. */
type TokenSource interface {
    Name() string

    Resolve(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (Token, error)
}
