package exception

import (
    "sync"

    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

/* Error answers for a nil receiver as Unwrap below does, and for the same producer: FromError(nil) answers a typed nil, and errors.Join skips only a nil interface, so errors.Join(FromError(a), FromError(b)) with one of them nil calls Error on the typed nil when the join is rendered — fmt recovers that dereference into <nil>, the join does not. */
func (instance *Error) Error() string {
    if nil == instance {
        return "error carries no value"
    }

    return instance.message
}

/* Unwrap is called by errors.Is and errors.As on EVERY link of a chain, so it is the one method of this type that runs on a nil receiver in ordinary use: FromError(nil) answers a typed nil, and a typed nil stored as another error's cause is a link the walk reaches before any caller's guard can. It answers nil on a nil receiver, so the walk ends there instead of dereferencing it — the typed-nil link itself stays in the chain, where errors.As matches it and the nil-receiver accessors of ExitError — its Unwrap included — answer for it; the guards on AsHttpException and the From* doors cover only the top of the chain. */
func (instance *Error) Unwrap() error {
    if nil == instance {
        return nil
    }

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
