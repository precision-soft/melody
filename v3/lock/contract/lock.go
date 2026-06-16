package contract

import (
    "time"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Lock interface {
    Acquire(runtimeInstance runtimecontract.Runtime) (bool, error)

    Release(runtimeInstance runtimecontract.Runtime) error

    Refresh(runtimeInstance runtimecontract.Runtime, ttl time.Duration) error
}

type Locker interface {
    CreateLock(name string, ttl time.Duration) Lock
}
