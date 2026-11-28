package contract

import (
    nethttp "net/http"
)

/* ForwardedHeadersPolicy decides whether forwarded headers from the direct peer are believed. Both fields are needed: an empty TrustedProxyList trusts nobody, whatever TrustForwardedHeaders says. The doors that accept it copy TrustedProxyList. X-Forwarded-Proto is read by its leftmost entry, so a trusted edge must overwrite the header rather than append to it. */
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
