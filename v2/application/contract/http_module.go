package contract

import (
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

type HttpModule interface {
    Module
    RegisterHttpRoutes(kernelInstance kernelcontract.Kernel)
}
