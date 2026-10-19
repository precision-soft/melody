package exception

import (
    "fmt"
    nethttp "net/http"
    "sync"

    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

type HttpException struct {
    statusCode int
    message    string
    /* stateMutex guards context and alreadyLogged: a memoized failure is shared across request goroutines. */
    stateMutex    sync.RWMutex
    context       exceptioncontract.Context
    causeErr      error
    alreadyLogged bool
}

/* Error answers a nil receiver with a placeholder, as Error.Error does. */
func (instance *HttpException) Error() string {
    if nil == instance {
        return "http exception carries no value"
    }

    if nil != instance.causeErr {
        return fmt.Sprintf("%s: %v", instance.message, instance.causeErr)
    }

    return instance.message
}

/* Unwrap answers nil on a nil receiver, as Error.Unwrap does. */
func (instance *HttpException) Unwrap() error {
    if nil == instance {
        return nil
    }

    return instance.causeErr
}

/* the accessors answer a nil receiver: Message the empty string, StatusCode zero */
func (instance *HttpException) Message() string {
    if nil == instance {
        return ""
    }

    return instance.message
}

func (instance *HttpException) Context() exceptioncontract.Context {
    if nil == instance {
        return nil
    }

    instance.stateMutex.RLock()
    defer instance.stateMutex.RUnlock()

    return copyStringMap(instance.context)
}

func (instance *HttpException) SetContext(context exceptioncontract.Context) {
    if nil == instance {
        return
    }

    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.context = copyStringMap(context)
}

func (instance *HttpException) SetContextValue(key string, value any) {
    if nil == instance {
        return
    }

    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    /* the zero value carries a nil map */
    if nil == instance.context {
        instance.context = make(exceptioncontract.Context)
    }

    instance.context[key] = value
}

func (instance *HttpException) CauseErr() error {
    if nil == instance {
        return nil
    }

    return instance.causeErr
}

func (instance *HttpException) StatusCode() int {
    if nil == instance {
        return 0
    }

    return instance.statusCode
}

func (instance *HttpException) AlreadyLogged() bool {
    if nil == instance {
        return false
    }

    instance.stateMutex.RLock()
    defer instance.stateMutex.RUnlock()

    return true == instance.alreadyLogged
}

func (instance *HttpException) MarkAsLogged() {
    if nil == instance {
        return
    }

    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.alreadyLogged = true
}

var _ exceptioncontract.ContextProvider = (*HttpException)(nil)
var _ exceptioncontract.AlreadyLogged = (*HttpException)(nil)

/* IsHttpException answers whether AsHttpException would find a usable exception. */
func IsHttpException(err error) bool {
    return nil != AsHttpException(err)
}

func AsHttpException(err error) *HttpException {
    /* a typed nil at the top is refused like a plain nil; deeper in the chain the nil-receiver Unwrap ends the walk */
    if true == isNilInterfaceValue(err) {
        return nil
    }

    var httpExceptionInstance *HttpException
    if true == chainHolds(err, &httpExceptionInstance) && nil != httpExceptionInstance {
        return httpExceptionInstance
    }

    return nil
}

func ValidationFailed(validationErrors any) *HttpException {
    httpException := NewHttpException(nethttp.StatusUnprocessableEntity, "validation failed")

    /* the kernel exception listener serves this key to the client */
    httpException.SetContextValue("validationErrors", validationErrors)

    return httpException
}
