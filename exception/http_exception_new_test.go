package exception

import (
    nethttp "net/http"
    "testing"
)

func TestHttpException_DefaultMessageWhenEmpty(t *testing.T) {
    ex := BadRequest("")
    if nethttp.StatusBadRequest != ex.StatusCode() {
        t.Fatalf("unexpected status code")
    }
    if "bad request" != ex.Message() {
        t.Fatalf("expected default message")
    }
}

func TestNewHttpException_StatusOutOfRange_Panics(t *testing.T) {
    for _, statusCode := range []int{0, 42, 99, 600, 999} {
        assertPanicsWithEmergency(t, "http status code out of range", func() {
            NewHttpException(statusCode, "boom")
        })

        assertPanicsWithEmergency(t, "http status code out of range", func() {
            NewHttpExceptionWithCause(statusCode, "boom", nil)
        })
    }
}

func TestNewHttpException_AcceptsTheRangeBoundaries(t *testing.T) {
    for _, statusCode := range []int{100, 599} {
        if statusCode != NewHttpException(statusCode, "boom").StatusCode() {
            t.Fatalf("expected status %d accepted", statusCode)
        }
    }
}

func TestNamedHttpExceptionConstructors_CarryTheirStatusAndDefaultMessage(t *testing.T) {
    constructorList := []struct {
        name           string
        constructor    func(string) *HttpException
        statusCode     int
        defaultMessage string
    }{
        {"BadRequest", BadRequest, nethttp.StatusBadRequest, "bad request"},
        {"Unauthorized", Unauthorized, nethttp.StatusUnauthorized, "unauthorized"},
        {"PaymentRequired", PaymentRequired, nethttp.StatusPaymentRequired, "payment required"},
        {"Forbidden", Forbidden, nethttp.StatusForbidden, "forbidden"},
        {"NotFound", NotFound, nethttp.StatusNotFound, "not found"},
        {"MethodNotAllowed", MethodNotAllowed, nethttp.StatusMethodNotAllowed, "method not allowed"},
        {"NotAcceptable", NotAcceptable, nethttp.StatusNotAcceptable, "not acceptable"},
        {"RequestTimeout", RequestTimeout, nethttp.StatusRequestTimeout, "request timeout"},
        {"Conflict", Conflict, nethttp.StatusConflict, "conflict"},
        {"Gone", Gone, nethttp.StatusGone, "gone"},
        {"UnprocessableEntity", UnprocessableEntity, nethttp.StatusUnprocessableEntity, "unprocessable entity"},
        {"TooManyRequests", TooManyRequests, nethttp.StatusTooManyRequests, "too many requests"},
        {"InternalServerError", InternalServerError, nethttp.StatusInternalServerError, "internal server error"},
        {"NotImplemented", NotImplemented, nethttp.StatusNotImplemented, "not implemented"},
        {"BadGateway", BadGateway, nethttp.StatusBadGateway, "bad gateway"},
        {"ServiceUnavailable", ServiceUnavailable, nethttp.StatusServiceUnavailable, "service unavailable"},
        {"GatewayTimeout", GatewayTimeout, nethttp.StatusGatewayTimeout, "gateway timeout"},
    }

    for _, constructorEntry := range constructorList {
        defaulted := constructorEntry.constructor("")

        if constructorEntry.statusCode != defaulted.StatusCode() {
            t.Fatalf("%s: expected status %d, got %d", constructorEntry.name, constructorEntry.statusCode, defaulted.StatusCode())
        }

        if constructorEntry.defaultMessage != defaulted.Message() {
            t.Fatalf("%s: expected default message %q, got %q", constructorEntry.name, constructorEntry.defaultMessage, defaulted.Message())
        }

        given := constructorEntry.constructor("the caller's own message")

        if "the caller's own message" != given.Message() {
            t.Fatalf("%s: expected the caller's message to be kept, got %q", constructorEntry.name, given.Message())
        }

        if constructorEntry.statusCode != given.StatusCode() {
            t.Fatalf("%s: expected status %d, got %d", constructorEntry.name, constructorEntry.statusCode, given.StatusCode())
        }
    }
}

func TestNamedHttpExceptionConstructors_DoNotShareAStatus(t *testing.T) {
    seenStatusCodeList := map[int]string{}

    constructorList := map[string]func(string) *HttpException{
        "BadRequest":          BadRequest,
        "Unauthorized":        Unauthorized,
        "PaymentRequired":     PaymentRequired,
        "Forbidden":           Forbidden,
        "NotFound":            NotFound,
        "MethodNotAllowed":    MethodNotAllowed,
        "NotAcceptable":       NotAcceptable,
        "RequestTimeout":      RequestTimeout,
        "Conflict":            Conflict,
        "Gone":                Gone,
        "UnprocessableEntity": UnprocessableEntity,
        "TooManyRequests":     TooManyRequests,
        "InternalServerError": InternalServerError,
        "NotImplemented":      NotImplemented,
        "BadGateway":          BadGateway,
        "ServiceUnavailable":  ServiceUnavailable,
        "GatewayTimeout":      GatewayTimeout,
    }

    for name, constructor := range constructorList {
        statusCode := constructor("").StatusCode()

        if previousName, exists := seenStatusCodeList[statusCode]; true == exists {
            t.Fatalf("%s and %s both answer with status %d", previousName, name, statusCode)
        }

        seenStatusCodeList[statusCode] = name
    }
}
