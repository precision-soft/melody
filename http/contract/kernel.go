package contract

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/container/contract"
)

/* ForwardedHeadersPolicy decides whether forwarded headers from the direct peer are believed. The doors that accept it — Kernel.SetForwardedHeadersPolicy, the forwarded client-ip resolver — copy TrustedProxyList rather than retain the caller's slice, so the trust decision every request reads cannot be rewritten from outside after the hand-over. X-Forwarded-Proto is honoured by its leftmost entry, which is only trustworthy when the trusted edge SETS the header, overwriting whatever the client sent; an edge that appends leaves the client's spelling leftmost and the client then chooses the scheme. */
type ForwardedHeadersPolicy struct {
    TrustForwardedHeaders bool
    TrustedProxyList      []string
}

/* SessionCookieSecurePolicy decides the Secure attribute of the session cookie. The zero value derives it from the scheme the forwarded-headers policy resolved, so a policy that does not mention it behaves as the framework always has; SessionCookieSecureAlways forces it on for a deployment whose proxy terminates TLS and forwards plaintext without being trusted for X-Forwarded-Proto. */
type SessionCookieSecurePolicy int

const (
    SessionCookieSecureFromScheme SessionCookieSecurePolicy = iota
    SessionCookieSecureAlways
    SessionCookieSecureNever
)

type SessionCookiePolicy struct {
    Path     string
    Domain   string
    SameSite nethttp.SameSite
    Secure   SessionCookieSecurePolicy
}

/* MethodPolicy decides the two answers the kernel synthesizes for methods no route declares: whether a HEAD request is served by the route registered for GET, and whether an OPTIONS request the application did not route is answered by the kernel with the Allow header it computes. Both default to true, which is what the framework has always done. An api that must answer 405 to OPTIONS, or one that serves HEAD itself, turns the matching half off through Kernel.SetMethodPolicy — the policy lives on the contract beside its two siblings because that is what an application holds. */
type MethodPolicy struct {
    HeadFallbackToGet bool
    AutomaticOptions  bool
}

type Kernel interface {
    Use(middlewares ...Middleware)

    SetNotFoundHandler(handler Handler)

    SetErrorHandler(handler ErrorHandler)

    SetForwardedHeadersPolicy(policy ForwardedHeadersPolicy)

    SetSessionCookiePolicy(policy SessionCookiePolicy)

    SetMethodPolicy(policy MethodPolicy)

    ServeHttp(serviceContainer containercontract.Container) nethttp.Handler
}
