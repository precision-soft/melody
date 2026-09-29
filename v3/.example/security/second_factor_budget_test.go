package security

import (
    "errors"
    "testing"

    melodysecurity "github.com/precision-soft/melody/v3/security"
)

func TestSecondFactorBudget_RefusesAPresentedCodeOnceTheAccountSpentItsBudget(t *testing.T) {
    budget := budgetOverPassword()

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        token, authenticateErr := budget.Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName))
        if nil != authenticateErr || false == token.IsAuthenticated() {
            t.Fatalf("expected attempt %d inside the budget to pass, got %v %v", attempt, token, authenticateErr)
        }
    }

    _, spentErr := budget.Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpRecoveryHeaderName))
    if false == errors.Is(spentErr, ErrSecondFactorBudgetSpent) {
        t.Fatalf("expected a recovery code past the budget refused as spent, got %v", spentErr)
    }
}

func TestSecondFactorBudget_SpendsNothingWithoutThePasswordOrWithoutACode(t *testing.T) {
    budget := budgetOverPassword()

    for attempt := 0; attempt < budgetTestAllowance+3; attempt++ {
        if _, refusedErr := budget.Authenticate(budgetRequestWithHeader(t, "wrong", melodysecurity.DefaultTotpCodeHeaderName)); nil != refusedErr {
            t.Fatalf("expected a refused password to answer no error, got %v", refusedErr)
        }

        if _, challengeErr := budget.Authenticate(budgetRequestWithHeader(t, "secret", "")); nil != challengeErr {
            t.Fatalf("expected a sign-in without a code to answer no error, got %v", challengeErr)
        }
    }

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        if _, authenticateErr := budget.Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)); nil != authenticateErr {
            t.Fatalf("expected the whole budget still available, attempt %d refused: %v", attempt, authenticateErr)
        }
    }
}

func TestSecondFactorBudget_ReleaseGivesTheAccountItsBudgetBack(t *testing.T) {
    budget := budgetOverPassword()

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        _, _ = budget.Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName))
    }

    budget.Release("user-editor")

    if _, authenticateErr := budget.Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)); nil != authenticateErr {
        t.Fatalf("expected the released budget to allow a code, got %v", authenticateErr)
    }
}

/* the budget is the account's own: another account that spent its budget leaves this one's whole */
func TestSecondFactorBudget_KeepsOneBudgetPerAccount(t *testing.T) {
    budget := budgetOverPassword()

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        _, _ = budget.Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName))
    }

    other := passwordRequest(t, "admin", "secret")
    other.HttpRequest().Header.Set(melodysecurity.DefaultTotpCodeHeaderName, "123456")

    if _, authenticateErr := budget.Authenticate(other); nil != authenticateErr {
        t.Fatalf("expected another account's code read while the editor's budget is spent, got %v", authenticateErr)
    }
}
