package exception

import (
    "errors"
    "fmt"
    nethttp "net/http"

    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

type HttpException struct {
    statusCode    int
    message       string
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
    return copyStringMap(instance.context)
}

func (instance *HttpException) SetContext(context exceptioncontract.Context) {
    instance.context = copyStringMap(context)
}

func (instance *HttpException) SetContextValue(key string, value any) {
    instance.context[key] = value
}

func (instance *HttpException) CauseErr() error {
    return instance.causeErr
}

func (instance *HttpException) StatusCode() int {
    return instance.statusCode
}

func (instance *HttpException) AlreadyLogged() bool {
    return true == instance.alreadyLogged
}

func (instance *HttpException) MarkAsLogged() {
    instance.alreadyLogged = true
}

var _ exceptioncontract.ContextProvider = (*HttpException)(nil)
var _ exceptioncontract.AlreadyLogged = (*HttpException)(nil)

func IsHttpException(err error) bool {
    if nil == err {
        return false
    }

    var httpExceptionInstance *HttpException
    return errors.As(err, &httpExceptionInstance)
}

func AsHttpException(err error) *HttpException {
    if nil == err {
        return nil
    }

    var httpExceptionInstance *HttpException
    if true == errors.As(err, &httpExceptionInstance) {
        return httpExceptionInstance
    }

    return nil
}

func ValidationFailed(validationErrors any) *HttpException {
    httpException := NewHttpException(nethttp.StatusUnprocessableEntity, "validation failed")

    httpException.SetContextValue("validationErrors", validationErrors)

    return httpException
}
