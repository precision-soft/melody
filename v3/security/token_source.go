package security

import (
    "github.com/precision-soft/melody/v3/event"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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

    /* the resolver is the application's function, and a nil pointer of its own token type reaches here as a non-nil interface: read as a live token it is published into the security context, where the first Roles() call panics */
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
                /* keep the authentication error as the cause: it carries the status the client should see (a 401 for bad credentials), which a bare dispatch error would replace with a 500 while hiding the real reason from the log */
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
