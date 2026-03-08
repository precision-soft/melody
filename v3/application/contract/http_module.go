package contract

import (
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

type HttpModule interface {
    Module
    RegisterHttpRoutes(kernelInstance kernelcontract.Kernel)
}
