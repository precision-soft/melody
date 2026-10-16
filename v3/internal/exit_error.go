package internal

import (
    "errors"

    "github.com/precision-soft/melody/v3/exception"
)

/* RecoveredExitError answers a recovered panic value as the exit error it carries; a typed nil is answered as absent. */
func RecoveredExitError(recoveredValue any) (*exception.ExitError, bool) {
    exitError, isExitError := recoveredValue.(*exception.ExitError)
    if false == isExitError || nil == exitError {
        return nil, false
    }

    return exitError, true
}

/* ExitErrorInChain answers the outermost exit error on an error's chain; a typed-nil match is answered as absent. */
func ExitErrorInChain(err error) (*exception.ExitError, bool) {
    var exitError *exception.ExitError
    if false == errors.As(err, &exitError) || nil == exitError {
        return nil, false
    }

    return exitError, true
}
