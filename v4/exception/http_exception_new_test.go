package exception

import (
    "errors"
    nethttp "net/http"
    "testing"
    "time"
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
            NewHttpError(statusCode, HttpErrorConfig{Message: "boom", Cause: nil})
        })
    }
}

func TestNewHttpException_AnInformationalStatusIsRefused(t *testing.T) {
    for _, statusCode := range []int{100, 103, 199} {
        assertPanicsWithEmergency(t, "http status code out of range", func() {
            NewHttpException(statusCode, "boom")
        })
    }
}

func TestNewHttpException_AcceptsTheRangeBoundaries(t *testing.T) {
    for _, statusCode := range []int{400, 599} {
        if statusCode != NewHttpException(statusCode, "boom").StatusCode() {
            t.Fatalf("expected status %d accepted", statusCode)
        }

        if statusCode != NewHttpError(statusCode, HttpErrorConfig{Message: "boom"}).StatusCode() {
            t.Fatalf("expected status %d accepted by NewHttpError", statusCode)
        }
    }
}

func TestNewHttpException_ASuccessOrRedirectStatusIsRefused(t *testing.T) {
    for _, statusCode := range []int{200, 204, 301, 302, 399} {
        assertPanicsWithEmergency(t, "http status code out of range", func() {
            NewHttpException(statusCode, "boom")
        })

        assertPanicsWithEmergency(t, "http status code out of range", func() {
            NewHttpError(statusCode, HttpErrorConfig{Message: "boom"})
        })
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

func TestNewHttpError_CarriesTheClientFacingIdentityAndTheCause(t *testing.T) {
    causeErr := errors.New("no row")

    httpError := NewHttpError(nethttp.StatusNotFound, HttpErrorConfig{Code: "order.notFound", Key: "error.order.not_found", Message: "order not found", Parameters: map[string]any{"id": 7}, Cause: causeErr})

    if nethttp.StatusNotFound != httpError.StatusCode() || "order not found" != httpError.Message() {
        t.Fatalf("expected the status and the message, got %d %q", httpError.StatusCode(), httpError.Message())
    }

    if "order.notFound" != httpError.Code() || "error.order.not_found" != httpError.Key() {
        t.Fatalf("expected the code and the key, got %q %q", httpError.Code(), httpError.Key())
    }

    if 7 != httpError.Parameters()["id"] {
        t.Fatalf("expected the parameters, got %v", httpError.Parameters())
    }

    if false == errors.Is(httpError, causeErr) {
        t.Fatalf("expected the cause on the chain")
    }
}

func TestNewHttpError_CopiesTheParametersOnTheWayInAndOut(t *testing.T) {
    parameters := map[string]any{"id": 7}

    httpError := NewHttpError(nethttp.StatusConflict, HttpErrorConfig{Code: "order.locked", Message: "order is locked", Parameters: parameters})

    parameters["id"] = 8
    answered := httpError.Parameters()
    answered["id"] = 9

    if 7 != httpError.Parameters()["id"] {
        t.Fatalf("expected the parameters to keep their constructed value, got %v", httpError.Parameters())
    }
}

func TestNewHttpError_RefusesAStatusOutOfRange(t *testing.T) {
    for _, statusCode := range []int{0, 199, 600} {
        assertPanicsWithEmergency(t, "http status code out of range", func() {
            NewHttpError(statusCode, HttpErrorConfig{Code: "code", Key: "key", Message: "message"})
        })
    }
}

func TestHttpException_AnExceptionWithoutAnIdentityAnswersEmptyValues(t *testing.T) {
    httpException := NewHttpException(nethttp.StatusBadRequest, "bad request")

    if "" != httpException.Code() || "" != httpException.Key() || 0 != len(httpException.Parameters()) {
        t.Fatalf("expected no identity, got %q %q %v", httpException.Code(), httpException.Key(), httpException.Parameters())
    }

    var unassigned *HttpException
    if "" != unassigned.Code() || "" != unassigned.Key() || nil != unassigned.Parameters() {
        t.Fatalf("expected a nil receiver to answer empty values")
    }
}

func TestNewHttpError_TheZeroConfigIsABareExceptionOfItsStatus(t *testing.T) {
    httpError := NewHttpError(nethttp.StatusNotFound, HttpErrorConfig{})

    if nethttp.StatusNotFound != httpError.StatusCode() || "" != httpError.Code() || "" != httpError.Key() || 0 != len(httpError.Parameters()) {
        t.Fatalf("expected a bare 404, got %d %q %q %v", httpError.StatusCode(), httpError.Code(), httpError.Key(), httpError.Parameters())
    }

    if nil != errors.Unwrap(httpError) {
        t.Fatalf("expected no cause, got %v", errors.Unwrap(httpError))
    }
}

func TestNewHttpError_CarriesTheRetryAfterDelay(t *testing.T) {
    if 7*time.Second != NewHttpError(nethttp.StatusServiceUnavailable, HttpErrorConfig{RetryAfter: 7 * time.Second}).RetryAfter() {
        t.Fatalf("expected the configured delay")
    }

    if 0 != NewHttpError(nethttp.StatusServiceUnavailable, HttpErrorConfig{}).RetryAfter() {
        t.Fatalf("expected no delay on the zero config")
    }

    if 0 != NewHttpException(nethttp.StatusServiceUnavailable, "down").RetryAfter() {
        t.Fatalf("expected no delay on a bare exception")
    }

    var nilException *HttpException
    if 0 != nilException.RetryAfter() {
        t.Fatalf("expected no delay on a nil receiver")
    }
}

func TestNewHttpError_RefusesANegativeRetryAfter(t *testing.T) {
    assertPanicsWithEmergency(t, "http error retry after is negative", func() {
        NewHttpError(nethttp.StatusServiceUnavailable, HttpErrorConfig{RetryAfter: -time.Second})
    })

    defer func() {
        recoveredError, _ := recover().(*Error)
        if nil == recoveredError || "HttpErrorConfig.RetryAfter" != recoveredError.Context()["field"] {
            t.Fatalf("expected the refusal to name the field, got %v", recoveredError)
        }
    }()

    NewHttpError(nethttp.StatusServiceUnavailable, HttpErrorConfig{RetryAfter: -time.Second})
}

func TestNewHttpException_TheRefusalNamesTheStatusAndTheRange(t *testing.T) {
    defer func() {
        recoveredError, ok := recover().(*Error)
        if false == ok || nil == recoveredError {
            t.Fatalf("expected the panic to carry an *Error")
        }

        context := recoveredError.Context()
        if 302 != context["statusCode"] || 400 != context["minimum"] || 599 != context["maximum"] {
            t.Fatalf("expected statusCode 302, minimum 400 and maximum 599, got %v", context)
        }
    }()

    NewHttpException(302, "boom")
}
