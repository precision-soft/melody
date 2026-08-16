package contract

import (
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

/* HttpModule registers routes on the kernel's router. The application's own routes — the ones RegisterHttpRoute queued — are already registered when this hook runs, so where a module route and a root route meet at dispatch, the root route wins the registration-order tie-break. */
type HttpModule interface {
    Module
    RegisterHttpRoutes(kernelInstance kernelcontract.Kernel)
}
