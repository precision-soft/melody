package http

import (
    "encoding/json"
    "errors"
    "io"
    "math"
    nethttp "net/http"

    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/validation"
)

func (instance *Request) BindJson(target any) error {
    if nil == target {
        return exception.NewError("bind target is nil", map[string]any{}, nil)
    }

    if nil == instance.httpRequest.Body {
        return exception.NewHttpException(400, "invalid request body")
    }

    maxBytes := maxRequestBodyBytes(instance)

    /* one byte beyond the limit is what tells an exactly-at-limit body apart from an oversized one; at the top of the int64 range that extra byte would wrap the reader's allowance negative and every body would read as empty, so the allowance saturates instead */
    overLimitAllowance := int64(maxBytes)
    if overLimitAllowance < math.MaxInt64 {
        overLimitAllowance++
    }

    limitedReader := io.LimitReader(instance.httpRequest.Body, overLimitAllowance)
    bodyBytes, err := io.ReadAll(limitedReader)
    if nil != err {
        var maxBytesError *nethttp.MaxBytesError
        if true == errors.As(err, &maxBytesError) {
            return exception.NewHttpException(nethttp.StatusRequestEntityTooLarge, "payload too large")
        }

        /* the cause distinguishes, in the log, a body that stopped arriving from one that never parsed — the response stays the same */
        return exception.NewHttpExceptionWithCause(nethttp.StatusBadRequest, "bad request", err)
    }

    if 0 == len(bodyBytes) {
        return exception.NewHttpException(400, "empty request body")
    }

    if maxBytes < len(bodyBytes) {
        return exception.NewHttpException(nethttp.StatusRequestEntityTooLarge, "payload too large")
    }

    err = json.Unmarshal(bodyBytes, target)
    if nil != err {
        /* the cause carries the decoder's own diagnosis — offending offset, field, type — which the flat message denied the log */
        return exception.NewHttpExceptionWithCause(400, "invalid json", err)
    }

    return nil
}

func (instance *Request) BindJsonAndValidate(target any) error {
    bindJsonErr := instance.BindJson(target)
    if nil != bindJsonErr {
        return bindJsonErr
    }

    validatorInstance := validation.ValidatorMustFromContainer(instance.runtimeInstance.Container())

    validationError := validatorInstance.Validate(target)
    if nil == validationError {
        return nil
    }

    validationErrors, ok := validationError.(validation.ValidationErrors)
    if false == ok {
        httpException := exception.BadRequest("validation failed")
        httpException.SetContext(
            exception.LogContext(validationError),
        )

        return httpException
    }

    httpException := exception.BadRequest("validation failed")
    httpException.SetContext(
        map[string]any{
            "errors": validationErrors,
        },
    )

    return httpException
}

func maxRequestBodyBytes(request httpcontract.Request) int {
    configuration := config.ConfigMustFromContainer(request.RuntimeInstance().Container())

    return configuration.Http().MaxRequestBodyBytes()
}
