package contract

import (
    nethttp "net/http"

    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* HttpHandlerDecorator wraps the fully built nethttp.Handler the http kernel produces, the outermost seam of the request lifecycle: unlike a middleware it observes every request, including 401/403 short-circuits, the panic-recovery path and responses written by event listeners. The first registered decorator is the outermost. */
type HttpHandlerDecorator func(next nethttp.Handler) nethttp.Handler

type HttpHandlerDecoratorModule interface {
    Module
    RegisterHttpHandlerDecorators(kernelInstance kernelcontract.Kernel) []HttpHandlerDecorator
}
