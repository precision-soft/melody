package contract

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* ForwardedHeadersPolicy controls trust in the direct peer’s forwarded headers. Setters copy TrustedProxyList. X-Forwarded-Proto uses the leftmost entry, so the trusted edge must overwrite client input rather than append to it. */
type ForwardedHeadersPolicy struct {
    TrustForwardedHeaders bool
    TrustedProxyList      []string
}

/* SessionCookieSecurePolicy defaults to the resolved request scheme. SessionCookieSecureAlways forces Secure, including behind an untrusted TLS-terminating proxy. */
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

/* MethodPolicy controls automatic HEAD-to-GET fallback and OPTIONS responses with Allow. Both default to true. Disable a behavior when the application must handle it itself. */
type MethodPolicy struct {
    HeadFallbackToGet bool
    AutomaticOptions  bool
}

/* Kernel is configured at boot. Mutators must be called before ServeHttp builds the handler; the framework kernel refuses later changes because request goroutines read the compiled state without synchronization. */
type Kernel interface {
    Use(middlewares ...Middleware)

    SetNotFoundHandler(handler Handler)

    SetErrorHandler(handler ErrorHandler)

    SetForwardedHeadersPolicy(policy ForwardedHeadersPolicy)

    SetSessionCookiePolicy(policy SessionCookiePolicy)

    SetMethodPolicy(policy MethodPolicy)

    ServeHttp(serviceContainer containercontract.Container) nethttp.Handler
}
