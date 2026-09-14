package contract

import (
    nethttp "net/http"

    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* HttpHandlerDecorator wraps the complete HTTP handler, including listener short-circuits and panic recovery. The first registered decorator is outermost. */
type HttpHandlerDecorator func(next nethttp.Handler) nethttp.Handler

type HttpHandlerDecoratorModule interface {
    Module
    RegisterHttpHandlerDecorators(kernelInstance kernelcontract.Kernel) []HttpHandlerDecorator
}
