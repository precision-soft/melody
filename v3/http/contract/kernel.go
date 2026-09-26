package contract

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* ForwardedHeadersPolicy decides whether forwarded headers from the direct peer are believed. The doors that accept it copy TrustedProxyList. X-Forwarded-Proto is read by its leftmost entry, so a trusted edge must overwrite the header rather than append to it. */
type ForwardedHeadersPolicy struct {
    TrustForwardedHeaders bool
    TrustedProxyList      []string
}

/* SessionCookieSecurePolicy decides the Secure attribute of the session cookie. The zero value derives it from the resolved scheme; SessionCookieSecureAlways forces it on, for a proxy that terminates TLS and is not trusted for X-Forwarded-Proto. */
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

/* MethodPolicy decides whether a HEAD request is served by the GET route and whether an unrouted OPTIONS is answered with the computed Allow header. Both default to true; Kernel.SetMethodPolicy turns either off. */
type MethodPolicy struct {
    HeadFallbackToGet bool
    AutomaticOptions  bool
}

/* Kernel is the http entry point an application configures at boot and then serves with. Every mutator is boot-only, since requests read the state it writes without synchronization; once ServeHttp has produced a handler, the framework kernel refuses such a call, naming the door. */
type Kernel interface {
    Use(middlewares ...Middleware)

    SetNotFoundHandler(handler Handler)

    SetErrorHandler(handler ErrorHandler)

    SetForwardedHeadersPolicy(policy ForwardedHeadersPolicy)

    SetSessionCookiePolicy(policy SessionCookiePolicy)

    SetMethodPolicy(policy MethodPolicy)

    ServeHttp(serviceContainer containercontract.Container) nethttp.Handler
}
