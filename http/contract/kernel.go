package contract

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/container/contract"
)

/* ForwardedHeadersPolicy decides whether forwarded headers from the direct peer are believed. The doors that accept it copy TrustedProxyList, so the trust decision cannot be rewritten from outside. X-Forwarded-Proto is read by its leftmost entry, which is trustworthy only when the trusted edge sets the header rather than appending to the client's. */
type ForwardedHeadersPolicy struct {
    TrustForwardedHeaders bool
    TrustedProxyList      []string
}

/* SessionCookieSecurePolicy decides the Secure attribute of the session cookie. The zero value derives it from the scheme the forwarded-headers policy resolved; SessionCookieSecureAlways forces it on for a proxy that terminates TLS and forwards plaintext without being trusted for X-Forwarded-Proto. */
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

/* MethodPolicy decides the two answers the kernel synthesizes for methods no route declares: whether HEAD is served by the GET route, and whether an unrouted OPTIONS is answered with the computed Allow header. Both default to true; an application turns either off through Kernel.SetMethodPolicy. */
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
