package http

import (
    "encoding/json"
    "io"
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

    limitedReader := io.LimitReader(instance.httpRequest.Body, int64(maxBytes)+1)
    bodyBytes, err := io.ReadAll(limitedReader)
    if nil != err {
        return exception.NewHttpException(nethttp.StatusBadRequest, "bad request")
    }

    if 0 == len(bodyBytes) {
        return exception.NewHttpException(400, "empty request body")
    }

    if maxBytes < len(bodyBytes) {
        return exception.NewHttpException(nethttp.StatusRequestEntityTooLarge, "payload too large")
    }

    err = json.Unmarshal(bodyBytes, target)
    if nil != err {
        return exception.NewHttpException(400, "invalid json")
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
