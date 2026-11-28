package http

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v4/event"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

func RecoverToError(recoveredValue any) error {
    if nil == recoveredValue {
        return nil
    }

    /* a typed-nil error takes the generic branch, as the exit handler's resolver reads it, so no reader dereferences its nil receiver inside the recovery defer */
    err, ok := recoveredValue.(error)
    if true == ok && false == internal.IsNilInterface(err) {
        return err
    }

    stringValue, ok := recoveredValue.(string)
    if true == ok {
        return exception.NewError(stringValue, map[string]any{}, nil)
    }

    return exception.NewError(
        "panic recovered",
        map[string]any{
            "value": internal.DescribeRecoveredValue(recoveredValue),
        },
        nil,
    )
}

/* debugErrorMessage renders an error's text under a recover, since a value whose Error() panics would raise a second panic while the first is rendered; a named rendering failure stands in for the text. */
func debugErrorMessage(err error) (message string) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        message = fmt.Sprintf("error message panicked: %v", recoveredValue)
    }()

    return err.Error()
}

/* debugKernelRequestFailureMessage names the refusal first, then what failed, so a 500 that replaced a refusal still shows the refusal in development */
func debugKernelRequestFailureMessage(refusal error, dispatchErr error) string {
    failurePart := debugErrorMessage(dispatchErr)

    if skippedErr, isSkipped := dispatchErr.(*event.RequiredListenerSkippedError); true == isSkipped {
        var melodyError *exception.Error
        if true == exception.ChainAs(skippedErr, &melodyError) && nil != melodyError {
            failurePart = failurePart + fmt.Sprintf(" (stopped by %v)", melodyError.Context()["stoppedByListener"])

            if causeErr := melodyError.CauseErr(); false == internal.IsNilInterface(causeErr) {
                failurePart = failurePart + ": " + debugCauseChainMessage(causeErr)
            }
        }
    }

    if true == internal.IsNilInterface(refusal) {
        return failurePart
    }

    refusalPart := debugErrorMessage(refusal)
    if true == exception.AnswersUnavailable(refusal) {
        refusalPart = "503 service unavailable"
    } else if httpException := exception.AsHttpException(refusal); nil != httpException {
        refusalPart = fmt.Sprintf("%d %s", httpException.StatusCode(), httpException.Message())
    }

    return "refused: " + refusalPart + "; then: " + failurePart
}

/* debugCauseChainMessage renders an error and the causes it wraps on one line, so a listener's failure reads past the dispatcher's wrapper */
func debugCauseChainMessage(err error) string {
    links := exception.BuildCauseChain(err, 8)
    if 0 == len(links) {
        return debugErrorMessage(err)
    }

    return strings.Join(links, ": ")
}
