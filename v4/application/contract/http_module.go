package contract

import (
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type HttpModule interface {
    Module
    RegisterHttpRoutes(kernelInstance kernelcontract.Kernel)
}
