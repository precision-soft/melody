package contract

import (
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

type HttpModule interface {
	Module
	RegisterHttpRoutes(kernelInstance kernelcontract.Kernel)
}
