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

/* bindJsonBody is the json-reading half Request.BindJson and JsonHandler share: the configured limit with its 413, the decoder's diagnosis as the refusal's cause, and the empty-body refusal. */
func bindJsonBody(instance httpcontract.Request, target any) error {
    if nil == target {
        return exception.NewError("bind target is nil", map[string]any{}, nil)
    }

    if nil == instance.HttpRequest() || nil == instance.HttpRequest().Body {
        return exception.NewHttpException(400, "invalid request body")
    }

    maxBytes := maxRequestBodyBytes(instance)

    /* one byte past the limit tells an at-limit body from an oversized one; the allowance saturates at the top of the int64 range */
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

        /* the cause tells a body that stopped arriving from one that never parsed; the response is the same */
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
        /* the cause carries the decoder's diagnosis: offset, field and type */
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

/* validateBoundBody is the validation half every json-binding door shares, so each attaches the collection under the validationErrors key. */
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

    /* the validationErrors key is the public half of the exception's context, which the exception listener projects into the response body */
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
