package security

import (
    "errors"
    "testing"

    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

/* scriptedRuntimeLimiter answers AllowWithRuntime as scripted and records the key; its plain Allow fails the test, since a runtime limiter is asked through the runtime door */
type scriptedRuntimeLimiter struct {
    t        *testing.T
    allowed  bool
    allowErr error
    keyList  []string
}

func (instance *scriptedRuntimeLimiter) Allow(key string) bool {
    instance.t.Fatalf("expected the runtime door asked, the plain Allow was asked for %q", key)

    return false
}

func (instance *scriptedRuntimeLimiter) AllowWithRuntime(runtimeInstance melodyruntimecontract.Runtime, key string) (bool, error) {
    instance.keyList = append(instance.keyList, key)

    return instance.allowed, instance.allowErr
}

func (instance *scriptedRuntimeLimiter) Reset(key string) {}

func TestSecondFactorBudget_RefusesWithUnavailableWhenAClosedLimiterCannotBeRead(t *testing.T) {
    storeErr := melodyexception.MarkLogged(melodyexception.NewError("redis rate limiter store failure", nil, errors.New("connection refused")))
    limiter := &scriptedRuntimeLimiter{t: t, allowed: false, allowErr: storeErr}

    token, authenticateErr := budgetOverPasswordWithLimiter(limiter).Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName))

    if nil != token || false == errors.Is(authenticateErr, ErrSecondFactorBudgetUnavailable) || false == errors.Is(authenticateErr, storeErr) {
        t.Fatalf("expected the unreadable budget refused as unavailable with the store's failure, got %v %v", token, authenticateErr)
    }

    if true == errors.Is(authenticateErr, ErrSecondFactorBudgetSpent) {
        t.Fatalf("expected an unreadable budget not answered as a spent one, got %v", authenticateErr)
    }

    if false == melodyexception.IsAlreadyLogged(authenticateErr) {
        t.Fatalf("expected the limiter's already-logged mark to stay readable through the refusal")
    }

    if 1 != len(limiter.keyList) || "account:user-editor" != limiter.keyList[0] {
        t.Fatalf("expected the account's key asked once, got %v", limiter.keyList)
    }
}

func TestSecondFactorBudget_HonoursAnOpenLimiterThatCannotBeRead(t *testing.T) {
    limiter := &scriptedRuntimeLimiter{t: t, allowed: true, allowErr: errors.New("connection refused")}

    token, authenticateErr := budgetOverPasswordWithLimiter(limiter).Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName))

    if nil != authenticateErr || nil == token || false == token.IsAuthenticated() {
        t.Fatalf("expected a fail-open limiter's outage to let the code be read, got %v %v", token, authenticateErr)
    }
}

func TestSecondFactorBudget_RefusesASpentBudgetThroughTheRuntimeLimiter(t *testing.T) {
    limiter := &scriptedRuntimeLimiter{t: t, allowed: false}

    _, spentErr := budgetOverPasswordWithLimiter(limiter).Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName))
    if false == errors.Is(spentErr, ErrSecondFactorBudgetSpent) {
        t.Fatalf("expected a spent budget refused as spent, got %v", spentErr)
    }

    allowing := &scriptedRuntimeLimiter{t: t, allowed: true}

    token, authenticateErr := budgetOverPasswordWithLimiter(allowing).Authenticate(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName))
    if nil != authenticateErr || nil == token || false == token.IsAuthenticated() {
        t.Fatalf("expected a budget with room to let the code be read, got %v %v", token, authenticateErr)
    }
}
