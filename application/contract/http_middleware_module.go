package contract

import (
	httpcontract "github.com/precision-soft/melody/http/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

type HttpMiddlewareModule interface {
	Module
	RegisterHttpMiddlewares(kernelInstance kernelcontract.Kernel, registrar HttpMiddlewareRegistrar)
}

type HttpMiddlewareRegistrar interface {
	Use(middlewares ...httpcontract.Middleware)

	UseWithPriority(priority int, middlewares ...httpcontract.Middleware)
}
