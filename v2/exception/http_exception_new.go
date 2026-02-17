package exception

import (
	nethttp "net/http"

	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
)

func NewHttpException(statusCode int, message string) *HttpException {
	return &HttpException{
		statusCode: statusCode,
		message:    message,
		context:    make(exceptioncontract.Context),
	}
}

func NewHttpExceptionWithCause(statusCode int, message string, causeErr error) *HttpException {
	return &HttpException{
		statusCode: statusCode,
		message:    message,
		context:    make(exceptioncontract.Context),
		causeErr:   causeErr,
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
