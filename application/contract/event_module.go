package contract

import (
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
)

type EventModule interface {
    Module
    RegisterEventSubscribers(kernelInstance kernelcontract.Kernel)
}
