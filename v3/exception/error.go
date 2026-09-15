package exception

import (
    "sync"

    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type Error struct {
    message string

    stateMutex    sync.RWMutex
    context       exceptioncontract.Context
    causeErr      error
    level         loggingcontract.Level
    alreadyLogged bool
}

func (instance *Error) Error() string {
    return instance.message
}

func (instance *Error) Unwrap() error {
    return instance.causeErr
}

func (instance *Error) Message() string {
    return instance.message
}

func (instance *Error) Context() exceptioncontract.Context {
    instance.stateMutex.RLock()
    defer instance.stateMutex.RUnlock()

    return copyStringMap(instance.context)
}

func (instance *Error) SetContext(context exceptioncontract.Context) {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.context = copyStringMap(context)
}

func (instance *Error) SetContextValue(key string, value any) {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    if nil == instance.context {
        instance.context = make(exceptioncontract.Context)
    }

    instance.context[key] = value
}

func (instance *Error) CauseErr() error {
    return instance.causeErr
}

func (instance *Error) Level() loggingcontract.Level {
    return instance.level
}

func (instance *Error) AlreadyLogged() bool {
    instance.stateMutex.RLock()
    defer instance.stateMutex.RUnlock()

    return true == instance.alreadyLogged
}

func (instance *Error) MarkAsLogged() {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.alreadyLogged = true
}

var _ exceptioncontract.ContextProvider = (*Error)(nil)
var _ exceptioncontract.AlreadyLogged = (*Error)(nil)
