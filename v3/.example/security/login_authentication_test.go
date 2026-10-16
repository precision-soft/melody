package security

import (
    "testing"

    melodysecurity "github.com/precision-soft/melody/v3/security"
)

func TestSecondFactorOutstanding_TellsAnUnansweredChallengeFromARefusedFactor(t *testing.T) {
    accepted := melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_EDITOR"})

    if false == SecondFactorOutstanding(melodysecurity.NewTwoFactorPendingToken(accepted)) {
        t.Fatal("expected a challenge never answered to be outstanding")
    }

    if true == SecondFactorOutstanding(melodysecurity.NewTwoFactorRejectedToken(accepted)) {
        t.Fatal("expected a refused second factor not to be outstanding")
    }

    if true == SecondFactorOutstanding(melodysecurity.NewAnonymousToken()) {
        t.Fatal("expected a refused password not to be outstanding")
    }
}

func TestLoginAuthentication_GivesTheBudgetBackOnlyForAnAcceptedSignInThatPresentedACode(t *testing.T) {
    budget := budgetOverPassword()
    login := NewLoginAuthentication(melodysecurity.NewAuthenticatorManager(budget), budget)

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        if _, authenticateErr := login.Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)); nil != authenticateErr {
            t.Fatalf("expected attempt %d to pass and give the budget back, got %v", attempt, authenticateErr)
        }
    }

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        if _, authenticateErr := budget.Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)); nil != authenticateErr {
            t.Fatalf("expected the budget given back after each accepted sign-in, attempt %d refused: %v", attempt, authenticateErr)
        }
    }
}
