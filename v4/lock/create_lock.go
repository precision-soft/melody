package lock

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

/* CreateLock asks the locker for the lock of a name and answers its refusal unchanged; a locker answering neither a lock nor an error is refused with "the locker answered no lock", naming the lock, so no caller calls a door of a nil lock. */
func CreateLock(locker lockcontract.Locker, name string, options lockcontract.CreateLockOptions) (lockcontract.Lock, error) {
    createdLock, createErr := locker.CreateLock(name, options)
    if nil != createErr {
        return nil, createErr
    }

    if true == internal.IsNilInterface(createdLock) {
        return nil, exception.NewError(
            "the locker answered no lock",
            exceptioncontract.Context{
                "name": name,
            },
            nil,
        )
    }

    return createdLock, nil
}
