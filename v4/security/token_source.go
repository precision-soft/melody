package security

import (
    "github.com/precision-soft/melody/v4/event"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func NewResolverTokenSource(resolver securitycontract.TokenResolver) *ResolverTokenSource {
    if nil == resolver {
        exception.Panic(exception.NewError("token resolver is nil", nil, nil))
    }

    return &ResolverTokenSource{resolver: resolver}
}

type ResolverTokenSource struct {
    resolver securitycontract.TokenResolver
}

func (instance *ResolverTokenSource) Name() string {
    return "tokenResolver"
}

func (instance *ResolverTokenSource) Resolve(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    token := instance.resolver(request)

    /* IsNilInterface: a typed nil token from the application's resolver would be published into the security context as live */
    if true == internal.IsNilInterface(token) {
        return NewAnonymousToken(), nil
    }

    return token, nil
}

var _ securitycontract.TokenSource = (*ResolverTokenSource)(nil)

func NewAuthenticatorTokenSource(manager *AuthenticatorManager) *AuthenticatorTokenSource {
    if nil == manager {
        exception.Panic(exception.NewError("authenticator manager is nil", nil, nil))
    }

    return &AuthenticatorTokenSource{manager: manager}
}

type AuthenticatorTokenSource struct {
    manager *AuthenticatorManager
}

func (instance *AuthenticatorTokenSource) Name() string {
    return "authenticatorManager"
}

func (instance *AuthenticatorTokenSource) Resolve(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    token, usedAuthenticator, err := instance.manager.Authenticate(request)
    if nil != err {
        if true == usedAuthenticator {
            eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Container())
            _, eventSecurityLoginFailureErr := eventDispatcher.DispatchName(
                runtimeInstance,
                securitycontract.EventSecurityLoginFailure,
                NewLoginFailureEvent(request, err),
            )
            if nil != eventSecurityLoginFailureErr {
                /* the authentication error stays the cause: it carries the status the client should see, which a bare dispatch error would turn into a 500 */
                return nil, exception.NewError(
                    "security login failure event dispatch failed",
                    exceptioncontract.Context{
                        "dispatchError": eventSecurityLoginFailureErr.Error(),
                    },
                    err,
                )
            }
        }

        return nil, err
    }

    /* IsNilInterface: a typed nil token falls through to the anonymous one */
    if true == internal.IsNilInterface(token) {
        token = NewAnonymousToken()
    }

    /* an authenticator that supported the request and answered no user rejected the credentials the request carried, a wrong api key among them: the failure event lets an audit or lockout listener see it, and the request goes on anonymous; a pending second factor is a rejection only when the token says the factor was supplied and refused, since a correct primary credential waiting for its code is the ordinary challenge */
    if true == usedAuthenticator && false == token.IsAuthenticated() {
        failureMessage := "security credentials rejected"
        if _, isPending := token.(securitycontract.TwoFactorPending); true == isPending {
            rejection, reportsRejection := token.(securitycontract.TwoFactorRejection)
            if false == reportsRejection || false == rejection.SecondFactorRejected() {
                return token, nil
            }

            failureMessage = "security second factor rejected"
        }

        eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Container())
        _, eventSecurityLoginFailureErr := eventDispatcher.DispatchName(
            runtimeInstance,
            securitycontract.EventSecurityLoginFailure,
            NewLoginFailureEvent(request, exception.NewError(failureMessage, nil, nil)),
        )
        if nil != eventSecurityLoginFailureErr {
            return nil, eventSecurityLoginFailureErr
        }

        return token, nil
    }

    if true == usedAuthenticator && true == token.IsAuthenticated() {
        eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Container())
        _, eventSecurityLoginSuccessErr := eventDispatcher.DispatchName(
            runtimeInstance,
            securitycontract.EventSecurityLoginSuccess,
            NewLoginSuccessEvent(request, token),
        )
        if nil != eventSecurityLoginSuccessErr {
            return nil, eventSecurityLoginSuccessErr
        }
    }

    return token, nil
}

var _ securitycontract.TokenSource = (*AuthenticatorTokenSource)(nil)
