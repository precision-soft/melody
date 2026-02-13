package contract

import (
	containercontract "github.com/precision-soft/melody/container/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

type ServiceModule interface {
	Module
	RegisterServices(kernelInstance kernelcontract.Kernel, registrar ServiceRegistrar)
}

type ServiceRegistrar interface {
	RegisterService(
		serviceName string,
		provider any,
		options ...containercontract.RegisterOption,
	)
}
