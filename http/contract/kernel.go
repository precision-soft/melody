package contract

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/container/contract"
)

type ForwardedHeadersPolicy struct {
    TrustForwardedHeaders bool
    TrustedProxyList      []string
}

type SessionCookiePolicy struct {
    Path     string
    Domain   string
    SameSite nethttp.SameSite
}

type Kernel interface {
    Use(middlewares ...Middleware)

    SetNotFoundHandler(handler Handler)

    SetErrorHandler(handler ErrorHandler)

    SetForwardedHeadersPolicy(policy ForwardedHeadersPolicy)

    SetSessionCookiePolicy(policy SessionCookiePolicy)

    ServeHttp(serviceContainer containercontract.Container) nethttp.Handler
}
