package exception

import (
    "sync"

    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type Error struct {
    message string
    /* stateMutex guards context and alreadyLogged, the fields written after construction: a failure the container memoizes is shared by concurrent requests. */
    stateMutex    sync.RWMutex
    context       exceptioncontract.Context
    causeErr      error
    level         loggingcontract.Level
    alreadyLogged bool
}

/* Error answers a nil receiver with a placeholder: a typed nil from FromError(nil) can be rendered through errors.Join. */
func (instance *Error) Error() string {
    if nil == instance {
        return "error carries no value"
    }

    return instance.message
}

/* Unwrap answers nil on a nil receiver, so errors.Is and errors.As end the walk at a typed-nil link instead of dereferencing it. */
func (instance *Error) Unwrap() error {
    if nil == instance {
        return nil
    }

    return instance.causeErr
}

/* the accessors answer a nil receiver: Message the empty string, Level error */
func (instance *Error) Message() string {
    if nil == instance {
        return ""
    }

    return instance.message
}

func (instance *Error) Context() exceptioncontract.Context {
    if nil == instance {
        return nil
    }

    instance.stateMutex.RLock()
    defer instance.stateMutex.RUnlock()

    return copyStringMap(instance.context)
}

func (instance *Error) SetContext(context exceptioncontract.Context) {
    if nil == instance {
        return
    }

    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.context = copyStringMap(context)
}

func (instance *Error) SetContextValue(key string, value any) {
    if nil == instance {
        return
    }

    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    /* the zero value carries a nil map, allocated at the first write */
    if nil == instance.context {
        instance.context = make(exceptioncontract.Context)
    }

    instance.context[key] = value
}

func (instance *Error) CauseErr() error {
    if nil == instance {
        return nil
    }

    return instance.causeErr
}

func (instance *Error) Level() loggingcontract.Level {
    if nil == instance {
        return loggingcontract.LevelError
    }

    return instance.level
}

func (instance *Error) AlreadyLogged() bool {
    if nil == instance {
        return false
    }

    instance.stateMutex.RLock()
    defer instance.stateMutex.RUnlock()

    return true == instance.alreadyLogged
}

func (instance *Error) MarkAsLogged() {
    if nil == instance {
        return
    }

    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.alreadyLogged = true
}

var _ exceptioncontract.ContextProvider = (*Error)(nil)
var _ exceptioncontract.AlreadyLogged = (*Error)(nil)
