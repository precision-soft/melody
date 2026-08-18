package exception

import (
    "sync"

    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type Error struct {
    message string
    /* stateMutex guards context and alreadyLogged, the two fields written after construction. Error handling stays single-threaded within one request by design, but a creation failure memoized by the container is reachable from the owner request and from every waiter request at once — the resolver hands each waiter a wrapper whose cause is the same instance, and the container's own Close returns one memoized error to every concurrent caller — so the mutable fields are locked rather than trusted to a premise that sharing already broke: an unlocked map write against a map iteration is a fatal runtime error no recover reaches. The immutable fields need no lock. */
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

    /* the zero value is constructible outside the constructors and carries a nil map; the first write allocates it instead of panicking on the assignment */
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
