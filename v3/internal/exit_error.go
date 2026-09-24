package internal

import (
    "errors"

    "github.com/precision-soft/melody/v3/exception"
)

/* RecoveredExitError answers a recovered panic value as the exit error it carries. A typed nil carries no code — reading it would dereference nil inside a recovery boundary — so it is answered as absent, and the boolean is what tells the two apart: the typed nil and the absence are the same pointer. */
func RecoveredExitError(recoveredValue any) (*exception.ExitError, bool) {
    exitError, isExitError := recoveredValue.(*exception.ExitError)
    if false == isExitError || nil == exitError {
        return nil, false
    }

    return exitError, true
}

/* ExitErrorInChain answers the outermost exit error on an error's chain. errors.As matches a typed-nil link and reports success, which would hand an exit a nil the constructor refuses, so a typed-nil match is answered as absent. */
func ExitErrorInChain(err error) (*exception.ExitError, bool) {
    var exitError *exception.ExitError
    if false == errors.As(err, &exitError) || nil == exitError {
        return nil, false
    }

    return exitError, true
}
