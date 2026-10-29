package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type HttpMiddlewareModule interface {
    Module
    RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar HttpMiddlewareRegistrar)
}

type MiddlewareFactory func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware

/* MiddlewareRegistration is what one Use installs: the middlewares and the factories, in the order given, middlewares first, at one priority. The zero value installs nothing at priority zero, the default; a lower priority sits further out. */
type MiddlewareRegistration struct {
    /* Priority orders the registration in the pipeline; 0 is the default, the static file server sits at -1000. */
    Priority int

    /* Middlewares are installed as given; a nil entry is refused. */
    Middlewares []httpcontract.Middleware

    /* Factories build their middleware against the booted kernel, after the Middlewares; a nil factory is refused here, and one that answers nil is refused at the build. */
    Factories []MiddlewareFactory
}

type HttpMiddlewareRegistrar interface {
    Use(registration MiddlewareRegistration)
}
