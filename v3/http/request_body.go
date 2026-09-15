package http

import (
    "encoding/json"
    "errors"
    "io"
    "math"
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/validation"
)

func (instance *Request) BindJson(target any) error {
    return bindJsonBody(instance, target)
}

func bindJsonBody(instance httpcontract.Request, target any) error {
    if nil == target {
        return exception.NewError("bind target is nil", map[string]any{}, nil)
    }

    if nil == instance.HttpRequest() || nil == instance.HttpRequest().Body {
        return exception.NewHttpException(400, "invalid request body")
    }

    maxBytes := maxRequestBodyBytes(instance)

    overLimitAllowance := int64(maxBytes)
    if overLimitAllowance < math.MaxInt64 {
        overLimitAllowance++
    }

    limitedReader := io.LimitReader(instance.HttpRequest().Body, overLimitAllowance)
    bodyBytes, err := io.ReadAll(limitedReader)
    if nil != err {
        var maxBytesError *nethttp.MaxBytesError
        if true == errors.As(err, &maxBytesError) {
            return exception.NewHttpException(nethttp.StatusRequestEntityTooLarge, "payload too large")
        }

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

        return exception.NewHttpExceptionWithCause(400, "invalid json", err)
    }

    return nil
}

func (instance *Request) BindJsonAndValidate(target any) error {
    bindJsonErr := instance.BindJson(target)
    if nil != bindJsonErr {
        return bindJsonErr
    }

    return validateBoundBody(instance.runtimeInstance, target)
}

func validateBoundBody(runtimeInstance runtimecontract.Runtime, target any) error {
    validatorInstance := validation.ValidatorMustFromContainer(runtimeInstance.Container())

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
            "validationErrors": validationErrors,
        },
    )

    return httpException
}

func maxRequestBodyBytes(request httpcontract.Request) int {
    configuration := config.ConfigMustFromContainer(request.RuntimeInstance().Container())

    return configuration.Http().MaxRequestBodyBytes()
}
