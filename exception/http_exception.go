package exception

import (
    "errors"
    "fmt"
    nethttp "net/http"
    "sync"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

type HttpException struct {
    statusCode int
    message    string
    /* @important stateMutex guards context and alreadyLogged for the reason Error documents: the instance can be shared across request goroutines through a memoized failure, and the immutable fields need no lock */
    stateMutex    sync.RWMutex
    context       exceptioncontract.Context
    causeErr      error
    alreadyLogged bool
}

func (instance *HttpException) Error() string {
    if nil != instance.causeErr {
        return fmt.Sprintf("%s: %v", instance.message, instance.causeErr)
    }

    return instance.message
}

func (instance *HttpException) Unwrap() error {
    return instance.causeErr
}

func (instance *HttpException) Message() string {
    return instance.message
}

func (instance *HttpException) Context() exceptioncontract.Context {
    instance.stateMutex.RLock()
    defer instance.stateMutex.RUnlock()

    return copyStringMap(instance.context)
}

func (instance *HttpException) SetContext(context exceptioncontract.Context) {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.context = copyStringMap(context)
}

func (instance *HttpException) SetContextValue(key string, value any) {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    /* the zero value is constructible outside the constructors and carries a nil map; the first write allocates it instead of panicking on the assignment */
    if nil == instance.context {
        instance.context = make(exceptioncontract.Context)
    }

    instance.context[key] = value
}

func (instance *HttpException) CauseErr() error {
    return instance.causeErr
}

func (instance *HttpException) StatusCode() int {
    return instance.statusCode
}

func (instance *HttpException) AlreadyLogged() bool {
    instance.stateMutex.RLock()
    defer instance.stateMutex.RUnlock()

    return true == instance.alreadyLogged
}

func (instance *HttpException) MarkAsLogged() {
    instance.stateMutex.Lock()
    defer instance.stateMutex.Unlock()

    instance.alreadyLogged = true
}

var _ exceptioncontract.ContextProvider = (*HttpException)(nil)
var _ exceptioncontract.AlreadyLogged = (*HttpException)(nil)

/* IsHttpException answers whether AsHttpException would find a usable exception, so the pair cannot disagree: a typed-nil match answered true here while AsHttpException answered nil, and a caller that trusted Is and then dereferenced panicked on the disagreement. */
func IsHttpException(err error) bool {
    return nil != AsHttpException(err)
}

func AsHttpException(err error) *HttpException {
    if nil == err {
        return nil
    }

    var httpExceptionInstance *HttpException
    if true == errors.As(err, &httpExceptionInstance) && nil != httpExceptionInstance {
        return httpExceptionInstance
    }

    return nil
}

func ValidationFailed(validationErrors any) *HttpException {
    httpException := NewHttpException(nethttp.StatusUnprocessableEntity, "validation failed")

    /* the errors key is the one the kernel exception listener serves to the client; the detail used to travel under a key nothing on the response path reads, so it reached no one */
    httpException.SetContextValue("errors", validationErrors)

    return httpException
}
