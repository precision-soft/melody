package security

import (
    "github.com/precision-soft/melody/event"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    securitycontract "github.com/precision-soft/melody/security/contract"
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
    if nil == token {
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
                /* @important keep the authentication error as the cause: it carries the status the client should see (a 401 for bad credentials), which a bare dispatch error would replace with a 500 while hiding the real reason from the log */
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

    if nil == token {
        return NewAnonymousToken(), nil
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
