package security

import (
    "errors"
    nethttp "net/http"
    "testing"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func loginFailureAnswerTable() map[string]struct {
    err    error
    events int
} {
    return map[string]struct {
        err    error
        events int
    }{
        "a plain error":          {err: errors.New("nil pointer in the authenticator"), events: 0},
        "an unauthorized error":  {err: exception.Unauthorized("bad password"), events: 1},
        "a forbidden error":      {err: exception.Forbidden("account locked"), events: 1},
        "a malformed request":    {err: exception.NewHttpException(nethttp.StatusBadRequest, "invalid credentials input"), events: 0},
        "a refused media type":   {err: exception.NewHttpException(nethttp.StatusUnsupportedMediaType, "json only"), events: 0},
        "a spent budget":         {err: exception.NewHttpException(nethttp.StatusTooManyRequests, "too many"), events: 0},
        "a second factor due":    {err: exception.NewHttpError(nethttp.StatusUnauthorized, exception.HttpErrorConfig{Message: "second factor required", Cause: ErrSecondFactorRequired}), events: 0},
        "a 503 error":            {err: exception.NewHttpException(nethttp.StatusServiceUnavailable, "down"), events: 0},
        "a 500 error":            {err: exception.NewHttpException(nethttp.StatusInternalServerError, "fault"), events: 0},
        "a marked error":         {err: exception.MarkInfrastructureFailure(errors.New("redis is down")), events: 0},
        "a marked client status": {err: exception.MarkInfrastructureFailure(exception.Unauthorized("remote said no")), events: 0},
    }
}

func countLoginFailureEvents(names []string) int {
    count := 0
    for _, name := range names {
        if securitycontract.EventSecurityLoginFailure == name {
            count++
        }
    }

    return count
}

func TestRaisesLoginFailure_TheTokenSourceRaisesTheEventForARefusedCredentialOnly(t *testing.T) {
    for name, turn := range loginFailureAnswerTable() {
        t.Run(name, func(t *testing.T) {
            runtimeInstance, dispatcher := newTokenSourceTestRuntime(t)
            dispatchedNameList := recordSecurityLoginEvents(dispatcher)

            tokenSource := NewAuthenticatorTokenSource(
                NewAuthenticatorManager([]securitycontract.Authenticator{
                    &testAuthenticator{
                        supportsCallback: func(request httpcontract.Request) bool { return true },
                        authenticateCallback: func(request httpcontract.Request) (securitycontract.Token, error) {
                            return nil, turn.err
                        },
                    },
                }),
            )

            _, err := tokenSource.Resolve(runtimeInstance, newFirewallTestRequest("/"))
            if false == errors.Is(err, turn.err) {
                t.Fatalf("expected the authenticator's error handed back, got %v", err)
            }

            if got := countLoginFailureEvents(*dispatchedNameList); turn.events != got {
                t.Fatalf("expected %d login failure events, got %d", turn.events, got)
            }
        })
    }
}

func TestRaisesLoginFailure_TheDoorsAuthenticatorRaisesTheEventForARefusedCredentialOnly(t *testing.T) {
    for name, turn := range loginFailureAnswerTable() {
        t.Run(name, func(t *testing.T) {
            fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
                config.LoginAuthenticator = &doorAuthenticator{err: turn.err}
            })

            fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

            if turn.events != *fixture.failures || 0 != fixture.login.calls {
                t.Fatalf("expected %d login failure events and no handler call, got %d and %d", turn.events, *fixture.failures, fixture.login.calls)
            }
        })
    }
}

func TestRaisesLoginFailure_TheDoorsHandlerRaisesTheEventForARefusedCredentialOnly(t *testing.T) {
    for name, turn := range loginFailureAnswerTable() {
        t.Run(name, func(t *testing.T) {
            fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
                config.LoginAuthenticator = &doorAuthenticator{token: NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)}
            })
            fixture.login.err = turn.err

            fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

            if turn.events != *fixture.failures || 1 != fixture.login.calls {
                t.Fatalf("expected %d login failure events after one handler call, got %d and %d", turn.events, *fixture.failures, fixture.login.calls)
            }
        })
    }
}

func TestLoginDoor_AFailedDispatchForAnAuthenticatorsRefusalKeepsTheStatusAndIsFiled(t *testing.T) {
    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = &doorAuthenticator{err: exception.Forbidden("account locked")}
    })
    fixture.kernel.eventDispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("the audit sink refused the record")
        },
        0,
    )

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusForbidden != recorder.Code {
        t.Fatalf("expected the authenticator's 403 kept, got %d: %s", recorder.Code, recorder.Body.String())
    }

    records := fixture.logger.recordsAtLevel(loggingcontract.LevelError)
    if 1 != len(records) || "security login failure event dispatch failed" != records[0].message {
        t.Fatalf("expected one Error record of the failed dispatch, got %v", fixture.logger.records)
    }
}

func TestLoginDoor_AFailedDispatchForAHandlersRefusalKeepsTheStatusAndIsFiled(t *testing.T) {
    fixture := newDoorFixture(t, func(config *CompiledFirewallConfig) {
        config.LoginAuthenticator = &doorAuthenticator{token: NewAuthenticatedToken("u1", []string{"ROLE_USER"}, nil)}
    })
    fixture.login.err = exception.Forbidden("account locked")
    fixture.kernel.eventDispatcher.AddListener(
        securitycontract.EventSecurityLoginFailure,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("the audit sink refused the record")
        },
        0,
    )

    recorder := fixture.serve(t, nethttp.MethodPost, doorLoginPath, nil)

    if nethttp.StatusForbidden != recorder.Code {
        t.Fatalf("expected the handler's 403 kept, got %d: %s", recorder.Code, recorder.Body.String())
    }

    records := fixture.logger.recordsAtLevel(loggingcontract.LevelError)
    if 1 != len(records) || "security login failure event dispatch failed" != records[0].message {
        t.Fatalf("expected one Error record of the failed dispatch, got %v", fixture.logger.records)
    }
}
