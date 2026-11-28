package security

import (
    "context"
    "errors"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/v4/http/middleware"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

func TestSecondFactorBudget_RefusesAPresentedCodeOnceTheAccountSpentItsBudget(t *testing.T) {
    budget := budgetOverPassword()

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        token, authenticateErr := budget.Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)))
        if nil != authenticateErr || false == token.IsAuthenticated() {
            t.Fatalf("expected attempt %d inside the budget to pass, got %v %v", attempt, token, authenticateErr)
        }
    }

    _, spentErr := budget.Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpRecoveryHeaderName)))
    if false == errors.Is(spentErr, ErrSecondFactorBudgetSpent) {
        t.Fatalf("expected a recovery code past the budget refused as spent, got %v", spentErr)
    }
}

func TestSecondFactorBudget_SpendsNothingWithoutThePasswordOrWithoutACode(t *testing.T) {
    budget := budgetOverPassword()

    for attempt := 0; attempt < budgetTestAllowance+3; attempt++ {
        if _, refusedErr := budget.Authenticate(withItsRuntime(budgetRequestWithHeader(t, "wrong", melodysecurity.DefaultTotpCodeHeaderName))); nil != refusedErr {
            t.Fatalf("expected a refused password to answer no error, got %v", refusedErr)
        }

        if _, challengeErr := budget.Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", ""))); nil != challengeErr {
            t.Fatalf("expected a sign-in without a code to answer no error, got %v", challengeErr)
        }
    }

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        if _, authenticateErr := budget.Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName))); nil != authenticateErr {
            t.Fatalf("expected the whole budget still available, attempt %d refused: %v", attempt, authenticateErr)
        }
    }
}

func TestSecondFactorBudget_ReleaseGivesTheAccountItsBudgetBack(t *testing.T) {
    budget := budgetOverPassword()

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        _, _ = budget.Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)))
    }

    if releaseErr := budget.Release(plainRequest(t).RuntimeInstance(), "user-editor"); nil != releaseErr {
        t.Fatalf("unexpected release failure: %v", releaseErr)
    }

    if _, authenticateErr := budget.Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName))); nil != authenticateErr {
        t.Fatalf("expected the released budget to allow a code, got %v", authenticateErr)
    }
}

/* the budget is the account's own: another account that spent its budget leaves this one's whole */
func TestSecondFactorBudget_KeepsOneBudgetPerAccount(t *testing.T) {
    budget := budgetOverPassword()

    for attempt := 0; attempt < budgetTestAllowance; attempt++ {
        _, _ = budget.Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)))
    }

    other := passwordRequest(t, "admin", "secret")
    other.HttpRequest().Header.Set(melodysecurity.DefaultTotpCodeHeaderName, "123456")

    if _, authenticateErr := budget.Authenticate(other.RuntimeInstance(), other); nil != authenticateErr {
        t.Fatalf("expected another account's code read while the editor's budget is spent, got %v", authenticateErr)
    }
}

type scriptedRuntimeLimiter struct {
    t          *testing.T
    allowed    bool
    retryAfter time.Duration
    allowErr   error
    resetErr   error
    keyList    []string
}

func (instance *scriptedRuntimeLimiter) Allow(runtimeInstance melodycontainercontract.Runtime, key string, _ melodyhttpcontract.AllowOptions) (melodyhttpcontract.RateLimitDecision, error) {
    instance.keyList = append(instance.keyList, key)

    return melodyhttpcontract.RateLimitDecision{Allowed: instance.allowed, RetryAfter: instance.retryAfter}, instance.allowErr
}

func (instance *scriptedRuntimeLimiter) Reset(runtimeInstance melodycontainercontract.Runtime, key string) error {
    instance.keyList = append(instance.keyList, key)

    return instance.resetErr
}

func TestSecondFactorBudget_AFailedReleaseIsReturned(t *testing.T) {
    resetErr := melodyexception.MarkInfrastructureFailure(errors.New("connection refused"))
    limiter := &scriptedRuntimeLimiter{t: t, allowed: true, resetErr: resetErr}

    releaseErr := budgetOverPasswordWithLimiter(limiter).Release(plainRequest(t).RuntimeInstance(), "user-editor")
    if false == errors.Is(releaseErr, resetErr) {
        t.Fatalf("expected the limiter's reset failure returned, got %v", releaseErr)
    }

    if 1 != len(limiter.keyList) || "account:user-editor" != limiter.keyList[0] {
        t.Fatalf("expected the account's key reset once, got %v", limiter.keyList)
    }
}

func TestSecondFactorBudget_AnOpenLimiterThatCannotBeReadIsJournaled(t *testing.T) {
    journal := &budgetJournalLogger{Logger: melodylogging.NewNopLogger()}
    request := budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)
    request.RuntimeInstance().Scope().MustOverrideProtectedInstance(melodylogging.ServiceLogger, journal, melodycontainer.OverrideOptions{})

    limiter := &scriptedRuntimeLimiter{t: t, allowed: true, allowErr: errors.New("connection refused")}
    if _, authenticateErr := budgetOverPasswordWithLimiter(limiter).Authenticate(request.RuntimeInstance(), request); nil != authenticateErr {
        t.Fatalf("expected the fail-open budget to let the code be read, got %v", authenticateErr)
    }

    if 1 != journal.errors {
        t.Fatalf("expected the unreadable budget journaled once, got %d", journal.errors)
    }
}

type budgetJournalLogger struct {
    melodyloggingcontract.Logger
    errors int
}

func (instance *budgetJournalLogger) Error(message string, context melodyloggingcontract.Context) {
    instance.errors++
}

func TestSecondFactorBudget_RefusesWithUnavailableWhenAClosedLimiterCannotBeRead(t *testing.T) {
    storeErr := melodyexception.NewError("redis rate limiter store failure", nil, melodyexception.MarkInfrastructureFailure(errors.New("connection refused")))
    limiter := &scriptedRuntimeLimiter{t: t, allowed: false, allowErr: storeErr}

    token, authenticateErr := budgetOverPasswordWithLimiter(limiter).Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)))

    if nil != token || false == errors.Is(authenticateErr, ErrSecondFactorBudgetUnavailable) || false == errors.Is(authenticateErr, storeErr) {
        t.Fatalf("expected the unreadable budget refused as unavailable with the store's failure, got %v %v", token, authenticateErr)
    }

    if true == errors.Is(authenticateErr, ErrSecondFactorBudgetSpent) {
        t.Fatalf("expected an unreadable budget not answered as a spent one, got %v", authenticateErr)
    }

    if false == melodyexception.AnswersUnavailable(authenticateErr) {
        t.Fatalf("expected the refusal answered as unavailable")
    }

    if 1 != len(limiter.keyList) || "account:user-editor" != limiter.keyList[0] {
        t.Fatalf("expected the account's key asked once, got %v", limiter.keyList)
    }
}

func TestSecondFactorBudget_HonoursAnOpenLimiterThatCannotBeRead(t *testing.T) {
    limiter := &scriptedRuntimeLimiter{t: t, allowed: true, allowErr: errors.New("connection refused")}

    token, authenticateErr := budgetOverPasswordWithLimiter(limiter).Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)))

    if nil != authenticateErr || nil == token || false == token.IsAuthenticated() {
        t.Fatalf("expected a fail-open limiter's outage to let the code be read, got %v %v", token, authenticateErr)
    }
}

func TestSecondFactorBudget_RefusesASpentBudgetThroughTheRuntimeLimiter(t *testing.T) {
    limiter := &scriptedRuntimeLimiter{t: t, allowed: false}

    _, spentErr := budgetOverPasswordWithLimiter(limiter).Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)))
    if false == errors.Is(spentErr, ErrSecondFactorBudgetSpent) {
        t.Fatalf("expected a spent budget refused as spent, got %v", spentErr)
    }

    allowing := &scriptedRuntimeLimiter{t: t, allowed: true}

    token, authenticateErr := budgetOverPasswordWithLimiter(allowing).Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)))
    if nil != authenticateErr || nil == token || false == token.IsAuthenticated() {
        t.Fatalf("expected a budget with room to let the code be read, got %v %v", token, authenticateErr)
    }
}

func TestSecondFactorBudget_ASpentBudgetCarriesTheDecisionsRetryAfter(t *testing.T) {
    limiter := &scriptedRuntimeLimiter{t: t, allowed: false, retryAfter: 42 * time.Second}

    _, spentErr := budgetOverPasswordWithLimiter(limiter).Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)))

    var refusal *SecondFactorBudgetSpentError
    if false == errors.As(spentErr, &refusal) || 42*time.Second != refusal.RetryAfter || false == errors.Is(spentErr, ErrSecondFactorBudgetSpent) {
        t.Fatalf("expected the spent refusal carrying the decision's 42s, got %v", spentErr)
    }

    if ErrSecondFactorBudgetSpent.Error() != spentErr.Error() {
        t.Fatalf("expected the refusal to read as the spent budget, got %q", spentErr.Error())
    }
}

func TestSecondFactorBudget_AnUnmarkedErrorBesideADenialIsReturnedUnmarked(t *testing.T) {
    allowErr := melodyexception.NewError("rate limiter call ended by the caller's deadline", nil, context.DeadlineExceeded)
    limiter := &scriptedRuntimeLimiter{t: t, allowed: false, allowErr: allowErr}

    _, authenticateErr := budgetOverPasswordWithLimiter(limiter).Authenticate(withItsRuntime(budgetRequestWithHeader(t, "secret", melodysecurity.DefaultTotpCodeHeaderName)))

    if false == errors.Is(authenticateErr, allowErr) || true == melodyexception.AnswersUnavailable(authenticateErr) || true == errors.Is(authenticateErr, ErrSecondFactorBudgetUnavailable) {
        t.Fatalf("expected the limiter's unmarked error returned as it is, got %v", authenticateErr)
    }
}

func TestSecondFactorBudget_SpendWithoutARuntimeIsNotAnOutage(t *testing.T) {
    budget := budgetOverPasswordWithLimiter(melodyhttpmiddleware.NewFixedWindowLimiter("test", 1, time.Minute, melodyhttpmiddleware.LimiterConfig{}))

    spendErr := budget.Spend(nil, "user-editor")
    if nil == spendErr || true == melodyexception.AnswersUnavailable(spendErr) {
        t.Fatalf("expected the nil runtime refused unmarked, got %v", spendErr)
    }
}
