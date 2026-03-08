package contract

import (
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

type EventModule interface {
    Module
    RegisterEventSubscribers(kernelInstance kernelcontract.Kernel)
}
