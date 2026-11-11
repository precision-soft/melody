package contract

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* ForwardedHeadersPolicy decides whether forwarded headers from the direct peer are believed. The doors that accept it copy TrustedProxyList. X-Forwarded-Proto is read by its leftmost entry, so a trusted edge must overwrite the header rather than append to it. */
type ForwardedHeadersPolicy struct {
    TrustForwardedHeaders bool
    TrustedProxyList      []string
}

/* SessionCookieSecurePolicy decides the Secure attribute of the session cookie. The zero value derives it from the resolved scheme, except under SameSite=None, where it is forced on since every current browser drops such a cookie without it; SessionCookieSecureAlways forces it on, for a proxy that terminates TLS and is not trusted for X-Forwarded-Proto; SessionCookieSecureNever forces it off, and is refused beside SameSite=None. */
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

/* MethodPolicy decides whether a HEAD request is served by the GET route and whether an unrouted OPTIONS is answered with the computed Allow header. The zero value is the framework's behaviour, both on; each flag turns its half off. */
type MethodPolicy struct {
    HeadFallbackDisabled bool

    AutomaticOptionsDisabled bool
}

/* SessionPolicy decides how the kernel serves the session of a request. The zero value is the framework's behaviour: a storage that cannot answer is an error to whoever reads the session, answered 503 with Retry-After. */
type SessionPolicy struct {
    /* EmptyOnOutage answers a reader an empty session marked unavailable when the storage cannot answer, in place of the error: nothing of it is saved, no cookie is written or expired, and one warning is recorded for the request. An error that is not an outage stays an error, and the firewall's sign-in and sign-out doors still refuse 503. */
    EmptyOnOutage bool
}

/* Kernel is the http entry point an application configures at boot and then serves with. Every mutator is boot-only, since requests read the state it writes without synchronization; once ServeHttp has produced a handler, the framework kernel refuses such a call, naming the door, and it refuses a policy it cannot serve, naming the field. */
type Kernel interface {
    Use(middlewares []Middleware)

    SetNotFoundHandler(handler Handler)

    SetForwardedHeadersPolicy(policy ForwardedHeadersPolicy)

    SetSessionCookiePolicy(policy SessionCookiePolicy)

    SetMethodPolicy(policy MethodPolicy)

    SetSessionPolicy(policy SessionPolicy)

    /* SetUnavailableRetryAfter installs the Retry-After of the 503 the error renderer answers an infrastructure failure with: 5s when absent, 1s to 1h in whole seconds, unlimited refused. */
    SetUnavailableRetryAfter(delay bound.Duration)

    ServeHttp(serviceContainer containercontract.Container) nethttp.Handler
}
