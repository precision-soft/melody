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

/* the accessors answer the nil receiver as Error and Unwrap above do, and as every accessor of ExitError does: the typed nil FromError(nil) produces is a link errors.As matches, and a caller that read it through the interface reached these before any guard. Message answers the empty string where Error answers a placeholder, because Message is the text the producer set and the nil set none, while Error is the rendering a chain shows; Level answers error rather than the zero level or unknown, because a record built from this link is a failure that reached a reader through a value that should not exist, and the journal weighs it as one. */
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

    /* the zero value is constructible outside the constructors and carries a nil map; the first write allocates it instead of panicking on the assignment */
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
