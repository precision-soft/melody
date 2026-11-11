package security

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* raisesLoginFailure: only a refused credential, 401 or 403, counts toward a lockout */
func raisesLoginFailure(err error) bool {
    if true == exception.AnswersUnavailable(err) {
        return false
    }

    if true == exception.ChainIs(err, ErrSecondFactorRequired) {
        return false
    }

    answer := exception.AsHttpException(err)

    return nil != answer && (nethttp.StatusUnauthorized == answer.StatusCode() || nethttp.StatusForbidden == answer.StatusCode())
}

/* fileLoginFailureDispatchError: a 401 or 403 is not recorded at error, so the failed listener would be seen nowhere else */
func fileLoginFailureDispatchError(
    runtimeInstance runtimecontract.Runtime,
    dispatchErr error,
    statusCode int,
    extra exceptioncontract.Context,
) {
    if nil == dispatchErr {
        return
    }

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Error(
            "security login failure event dispatch failed",
            exception.LogContext(
                exception.NewError("security login failure event dispatch failed", extra, dispatchErr),
                exception.LogContextConfig{Extra: exceptioncontract.Context{"statusCode": statusCode}},
            ),
        )
    }
}
