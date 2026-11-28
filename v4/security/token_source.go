package security

import (
    "errors"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* ResolverTokenSourceConfig: the zero value declares the source stateful, the protected answer. */
type ResolverTokenSourceConfig struct {
    /* Stateless declares that the resolver reads only a credential the caller presents on every request, nothing a browser attaches by itself; a resolver reading a session leaves it false. */
    Stateless bool
}

func NewResolverTokenSource(resolver securitycontract.TokenResolver, config ResolverTokenSourceConfig) *ResolverTokenSource {
    if nil == resolver {
        exception.Panic(exception.NewError("token resolver is nil", nil, nil))
    }

    return &ResolverTokenSource{resolver: resolver, config: config}
}

type ResolverTokenSource struct {
    resolver securitycontract.TokenResolver
    config   ResolverTokenSourceConfig
}

func (instance *ResolverTokenSource) Name() string {
    return "tokenResolver"
}

func (instance *ResolverTokenSource) Stateless() bool {
    return instance.config.Stateless
}

func (instance *ResolverTokenSource) Resolve(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    token, resolveErr := instance.resolver(runtimeInstance, request)
    if false == internal.IsNilInterface(resolveErr) {
        return nil, resolveErr
    }

    /* IsNilInterface: a typed nil token from the application's resolver would be published into the security context as live */
    if true == internal.IsNilInterface(token) {
        return NewAnonymousToken(), nil
    }

    return token, nil
}

var _ securitycontract.TokenSource = (*ResolverTokenSource)(nil)

/* AuthenticatorTokenSourceConfig: the zero value declares the source stateful, the protected answer, since its authenticators are the application's. */
type AuthenticatorTokenSourceConfig struct {
    /* Stateless declares that every authenticator of the manager reads only a credential the caller presents on every request, nothing a browser attaches by itself. */
    Stateless bool
}

func NewAuthenticatorTokenSource(manager *AuthenticatorManager, config AuthenticatorTokenSourceConfig) *AuthenticatorTokenSource {
    if nil == manager {
        exception.Panic(exception.NewError("authenticator manager is nil", nil, nil))
    }

    return &AuthenticatorTokenSource{manager: manager, config: config}
}

type AuthenticatorTokenSource struct {
    manager *AuthenticatorManager
    config  AuthenticatorTokenSourceConfig
}

func (instance *AuthenticatorTokenSource) Name() string {
    return "authenticatorManager"
}

func (instance *AuthenticatorTokenSource) Stateless() bool {
    return instance.config.Stateless
}

func (instance *AuthenticatorTokenSource) Resolve(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    token, usedAuthenticator, err := instance.manager.Authenticate(runtimeInstance, request)
    if nil != err {
        if true == usedAuthenticator && true == raisesLoginFailure(err) {
            eventDispatcher := event.EventDispatcherMustFromRuntime(runtimeInstance)
            _, eventSecurityLoginFailureErr := eventDispatcher.DispatchName(
                runtimeInstance,
                securitycontract.EventSecurityLoginFailure,
                NewLoginFailureEvent(request, err),
            )
            fileLoginFailureDispatchError(
                runtimeInstance,
                eventSecurityLoginFailureErr,
                exception.AsHttpException(err).StatusCode(),
                exceptioncontract.Context{"tokenSourceName": instance.Name()},
            )
        }

        return nil, err
    }

    /* IsNilInterface: a typed nil token falls through to the anonymous one */
    if true == internal.IsNilInterface(token) {
        token = NewAnonymousToken()
    }

    /* a credential the authenticator could not check is neither a rejection nor a sign-in */
    if _, unavailable := AuthenticationUnavailableCause(token); true == unavailable {
        return token, nil
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

        var failureErr error = exception.NewError(failureMessage, nil, nil)

        /* a locked account is refused on every route, a public one included, since the request carried a factor for it */
        var lockedRefusal error
        if rejection := secondFactorLock(token); nil != rejection {
            lockedRefusal = newSecondFactorLockedRefusal(rejection, "tokenSourceName", instance.Name())
            failureErr = lockedRefusal
        }

        eventDispatcher := event.EventDispatcherMustFromRuntime(runtimeInstance)
        _, eventSecurityLoginFailureErr := eventDispatcher.DispatchName(
            runtimeInstance,
            securitycontract.EventSecurityLoginFailure,
            NewLoginFailureEvent(request, failureErr),
        )
        if nil != eventSecurityLoginFailureErr {
            /* the rejection is kept beside the dispatch error, so the record says which login failed */
            return nil, exception.NewError(
                "security login failure event dispatch failed",
                exceptioncontract.Context{
                    "dispatchError": eventSecurityLoginFailureErr.Error(),
                },
                errors.Join(failureErr, eventSecurityLoginFailureErr),
            )
        }

        if nil != lockedRefusal {
            return nil, lockedRefusal
        }

        return token, nil
    }

    if true == usedAuthenticator && true == token.IsAuthenticated() {
        eventDispatcher := event.EventDispatcherMustFromRuntime(runtimeInstance)
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
