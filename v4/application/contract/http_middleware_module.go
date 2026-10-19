package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type HttpMiddlewareModule interface {
    Module
    RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar HttpMiddlewareRegistrar)
}

type HttpMiddlewareRegistrar interface {
    Use(middlewares ...httpcontract.Middleware)

    UseWithPriority(priority int, middlewares ...httpcontract.Middleware)
}
