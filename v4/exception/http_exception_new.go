package exception

import (
    nethttp "net/http"

    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

func NewHttpException(statusCode int, message string) *HttpException {
    refuseStatusCodeOutOfRange(statusCode)

    return &HttpException{
        statusCode: statusCode,
        message:    message,
        context:    make(exceptioncontract.Context),
    }
}

/* HttpErrorConfig declares the client-facing identity of an http exception; every field is optional, and the zero value is a bare exception of its status. */
type HttpErrorConfig struct {
    /* Code is the stable identifier a client branches on; left out of the error object when empty. */
    Code string

    /* Key is the translation key a client translates; left out of the error object when empty. */
    Key string

    /* Message is the human-readable message. */
    Message string

    /* Parameters are the values the key's translation interpolates; copied at construction, left out when empty. */
    Parameters map[string]any

    /* Cause is the wrapped error; nil for none. */
    Cause error
}

/* NewHttpError builds an http exception answering statusCode, refused outside 200..599, with the identity the config declares: the error renderer serves code, key and parameters inside the error object beside the message, each left out when empty, so a client branches on the code and translates the key without parsing the message. The framework translates nothing. */
func NewHttpError(statusCode int, config HttpErrorConfig) *HttpException {
    refuseStatusCodeOutOfRange(statusCode)

    return &HttpException{
        statusCode: statusCode,
        message:    config.Message,
        code:       config.Code,
        key:        config.Key,
        parameters: copyStringMap(config.Parameters),
        context:    make(exceptioncontract.Context),
        causeErr:   config.Cause,
    }
}

/* refuseStatusCodeOutOfRange refuses at construction a status outside 200–599, the classes an http exception can answer with: net/http's WriteHeader would panic deep in the response path below 100 and above 999, and an informational status is not a final answer, so the writer serves the error body under a 200. */
func refuseStatusCodeOutOfRange(statusCode int) {
    if 200 > statusCode || 599 < statusCode {
        Panic(
            NewEmergency(
                "http status code out of range",
                exceptioncontract.Context{
                    "statusCode": statusCode,
                },
                nil,
            ),
        )
    }
}

func BadRequest(message string) *HttpException {
    if "" == message {
        message = "bad request"
    }
    return NewHttpException(nethttp.StatusBadRequest, message)
}

func Unauthorized(message string) *HttpException {
    if "" == message {
        message = "unauthorized"
    }
    return NewHttpException(nethttp.StatusUnauthorized, message)
}

func PaymentRequired(message string) *HttpException {
    if "" == message {
        message = "payment required"
    }
    return NewHttpException(nethttp.StatusPaymentRequired, message)
}

func Forbidden(message string) *HttpException {
    if "" == message {
        message = "forbidden"
    }
    return NewHttpException(nethttp.StatusForbidden, message)
}

func NotFound(message string) *HttpException {
    if "" == message {
        message = "not found"
    }
    return NewHttpException(nethttp.StatusNotFound, message)
}

func MethodNotAllowed(message string) *HttpException {
    if "" == message {
        message = "method not allowed"
    }
    return NewHttpException(nethttp.StatusMethodNotAllowed, message)
}

func NotAcceptable(message string) *HttpException {
    if "" == message {
        message = "not acceptable"
    }
    return NewHttpException(nethttp.StatusNotAcceptable, message)
}

func RequestTimeout(message string) *HttpException {
    if "" == message {
        message = "request timeout"
    }
    return NewHttpException(nethttp.StatusRequestTimeout, message)
}

func Conflict(message string) *HttpException {
    if "" == message {
        message = "conflict"
    }
    return NewHttpException(nethttp.StatusConflict, message)
}

func Gone(message string) *HttpException {
    if "" == message {
        message = "gone"
    }
    return NewHttpException(nethttp.StatusGone, message)
}

func UnprocessableEntity(message string) *HttpException {
    if "" == message {
        message = "unprocessable entity"
    }
    return NewHttpException(nethttp.StatusUnprocessableEntity, message)
}

func TooManyRequests(message string) *HttpException {
    if "" == message {
        message = "too many requests"
    }
    return NewHttpException(nethttp.StatusTooManyRequests, message)
}

func InternalServerError(message string) *HttpException {
    if "" == message {
        message = "internal server error"
    }
    return NewHttpException(nethttp.StatusInternalServerError, message)
}

func NotImplemented(message string) *HttpException {
    if "" == message {
        message = "not implemented"
    }
    return NewHttpException(nethttp.StatusNotImplemented, message)
}

func BadGateway(message string) *HttpException {
    if "" == message {
        message = "bad gateway"
    }
    return NewHttpException(nethttp.StatusBadGateway, message)
}

func ServiceUnavailable(message string) *HttpException {
    if "" == message {
        message = "service unavailable"
    }
    return NewHttpException(nethttp.StatusServiceUnavailable, message)
}

func GatewayTimeout(message string) *HttpException {
    if "" == message {
        message = "gateway timeout"
    }
    return NewHttpException(nethttp.StatusGatewayTimeout, message)
}
