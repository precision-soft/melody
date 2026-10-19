package contract

import (
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

/* HttpModule registers routes on the kernel's router. The application's own routes are already registered when this hook runs, so a root route wins the registration-order tie-break against a module route. */
type HttpModule interface {
    Module
    RegisterHttpRoutes(kernelInstance kernelcontract.Kernel)
}
