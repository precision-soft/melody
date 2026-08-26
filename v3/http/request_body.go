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

/* bindJsonBody is the one json-reading half of the framework: the request method above and the typed JsonHandler both answer through it, so the configured body limit with its 413, the decoder's own diagnosis kept as the refusal's cause and the empty-body refusal are read once rather than written twice and drifted apart. */
func bindJsonBody(instance httpcontract.Request, target any) error {
    if nil == target {
        return exception.NewError("bind target is nil", map[string]any{}, nil)
    }

    if nil == instance.HttpRequest() || nil == instance.HttpRequest().Body {
        return exception.NewHttpException(400, "invalid request body")
    }

    maxBytes := maxRequestBodyBytes(instance)

    /* one byte beyond the limit is what tells an exactly-at-limit body apart from an oversized one; at the top of the int64 range that extra byte would wrap the reader's allowance negative and every body would read as empty, so the allowance saturates instead */
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

    return validateBoundBody(instance.runtimeInstance, target)
}

/* validateBoundBody is the one validation half every json-binding door answers through — the request method above and the typed JsonHandler alike. Held in two places it drifted: the typed handler flattened the collection into the exception's message, so its client read a joined sentence where the binding method's client read the validationErrors array, and the listener's rule-wiring classification — which reads exactly that context key — never fired for it, leaving a route broken by a struct-tag typo recorded at warning among the users who mistyped their address. */
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

    /* the validationErrors key is the public half of the exception's context: the kernel exception listener projects it into the response body, so the per-field detail the validator computed reaches the client under the one key that names it */
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
