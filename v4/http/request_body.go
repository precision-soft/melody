package http

import (
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "math"
    nethttp "net/http"
    "reflect"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/validation"
)

/* ErrRequestBodyTooLarge heads the cause chain of every 413 the framework answers for a body past its cap, the net/http MaxBytesError behind it when one was raised, so a listener matches one error whatever read raised it. */
var ErrRequestBodyTooLarge = errors.New("request body too large")

/* newRequestBodyTooLargeError builds the 413 of a body past its cap, the read's own error, when there is one, kept behind the sentinel */
func newRequestBodyTooLargeError(maxBodyBytes int, readErr error) error {
    cause := ErrRequestBodyTooLarge
    if nil != readErr {
        cause = fmt.Errorf("%w: %w", ErrRequestBodyTooLarge, readErr)
    }

    refusal := exception.NewHttpError(nethttp.StatusRequestEntityTooLarge, exception.HttpErrorConfig{Message: "payload too large", Cause: cause})
    refusal.SetContextValue("maxBodyBytes", maxBodyBytes)

    return refusal
}

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

    maxBytes := instance.MaxBodyBytes()

    /* one byte past the limit tells an at-limit body from an oversized one; the allowance saturates at the top of the int64 range */
    overLimitAllowance := int64(maxBytes)
    if overLimitAllowance < math.MaxInt64 {
        overLimitAllowance++
    }

    limitedReader := io.LimitReader(instance.HttpRequest().Body, overLimitAllowance)
    bodyBytes, err := io.ReadAll(limitedReader)
    if nil != err {
        var maxBytesError *nethttp.MaxBytesError
        if true == exception.ChainAs(err, &maxBytesError) {
            return newRequestBodyTooLargeError(maxBytes, err)
        }

        /* the cause tells a body that stopped arriving from one that never parsed; the response is the same */
        return exception.NewHttpError(nethttp.StatusBadRequest, exception.HttpErrorConfig{Message: "bad request", Cause: err})
    }

    if 0 == len(bodyBytes) {
        return exception.NewHttpException(400, "empty request body")
    }

    if maxBytes < len(bodyBytes) {
        return newRequestBodyTooLargeError(maxBytes, nil)
    }

    err = json.Unmarshal(bodyBytes, target)
    if nil != err {
        /* the cause carries the decoder's diagnosis: offset, field and type */
        return exception.NewHttpError(400, exception.HttpErrorConfig{Message: "invalid json", Cause: err})
    }

    return nil
}

func (instance *Request) BindJsonAndValidate(target any) error {
    bindJsonErr := instance.BindJson(target)
    if nil != bindJsonErr {
        return bindJsonErr
    }

    /* a literal null leaves a nilable target's value nil, which the validator passes, so it is refused here as the typed json handler refuses it */
    if true == boundTargetIsNil(target) {
        return exception.NewHttpException(nethttp.StatusBadRequest, "empty request body")
    }

    return validateBoundBody(instance.runtimeInstance, target)
}

/* validateBoundBody is the validation half every json-binding door shares, so each attaches the collection under the validationErrors key. */
func validateBoundBody(runtimeInstance containercontract.Runtime, target any) error {
    validatorInstance := validation.ValidatorMustFromRuntime(runtimeInstance)

    return boundBodyValidationAnswer(validatorInstance.Validate(runtimeInstance, target))
}

/* boundBodyValidationAnswer answers the client's 400 for validation errors alone; any other error of the validator, the refusal of a runtime carrying no context, is a server failure and travels as a plain error, so it answers 500 and is journaled as one. */
func boundBodyValidationAnswer(validationError error) error {
    if nil == validationError {
        return nil
    }

    validationErrors, ok := validationError.(validation.ValidationErrors)
    if false == ok {
        return exception.NewError("the request body could not be validated", nil, validationError)
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

/* boundTargetIsNil reports whether the value a json binding wrote through the target pointer is nil, every kind a json null can leave nil */
func boundTargetIsNil(target any) bool {
    targetValue := reflect.ValueOf(target)
    if reflect.Ptr != targetValue.Kind() || true == targetValue.IsNil() {
        return false
    }

    return boundBodyIsNil(targetValue.Elem().Interface())
}
