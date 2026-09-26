package internal

import (
    "fmt"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
)

func TestRecoveredExitError_AnswersTheExitErrorItCarries(t *testing.T) {
    carried := exception.NewExitError(3, exception.NewError("stop", nil, nil))

    exitError, isExit := RecoveredExitError(carried)
    if false == isExit || carried != exitError {
        t.Fatalf("expected the carried exit error, got %v %v", exitError, isExit)
    }
}

func TestRecoveredExitError_AnswersATypedNilAsAbsent(t *testing.T) {
    if _, isExit := RecoveredExitError((*exception.ExitError)(nil)); true == isExit {
        t.Fatalf("expected a typed nil answered as absent")
    }
}

func TestRecoveredExitError_AnswersAnotherValueAsAbsent(t *testing.T) {
    if _, isExit := RecoveredExitError("a plain panic"); true == isExit {
        t.Fatalf("expected a plain panic value answered as absent")
    }
}

func TestExitErrorInChain_AnswersTheOutermostExitErrorOnTheChain(t *testing.T) {
    outer := exception.NewExitError(4, exception.NewError("outer", nil, nil))
    wrapped := fmt.Errorf("wrapped: %w", outer)

    exitError, isExit := ExitErrorInChain(wrapped)
    if false == isExit || outer != exitError {
        t.Fatalf("expected the exit error on the chain, got %v %v", exitError, isExit)
    }
}

func TestExitErrorInChain_AnswersATypedNilLinkAsAbsent(t *testing.T) {
    wrapped := fmt.Errorf("wrapped: %w", (*exception.ExitError)(nil))

    if _, isExit := ExitErrorInChain(wrapped); true == isExit {
        t.Fatalf("expected a typed-nil link answered as absent")
    }
}

func TestExitErrorInChain_AnswersAChainWithoutOneAsAbsent(t *testing.T) {
    if _, isExit := ExitErrorInChain(exception.NewError("plain", nil, nil)); true == isExit {
        t.Fatalf("expected a chain without an exit error answered as absent")
    }
}
