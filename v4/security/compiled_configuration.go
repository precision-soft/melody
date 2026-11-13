package security

import (
    "errors"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/event"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    httpmiddleware "github.com/precision-soft/melody/v4/http/middleware"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* CompiledFirewallConfig is everything a compiled firewall is built from, for a caller that compiles by hand; securityconfig.Compile builds it from a declaration. Name and Matcher are required; a nil Rules entry is refused by its index; a Source left empty reads as SourceNone. */
type CompiledFirewallConfig struct {
    Name               string
    Matcher            securitycontract.Matcher
    MatcherDescription string
    Rules              []securitycontract.Rule
    TokenSource        securitycontract.TokenSource

    AccessControl         *AccessControl
    AccessDecisionManager securitycontract.AccessDecisionManager
    RoleHierarchy         *RoleHierarchy
    EntryPoint            securitycontract.EntryPoint
    AccessDeniedHandler   securitycontract.AccessDeniedHandler

    LoginPath     string
    LogoutPath    string
    LoginHandler  securitycontract.LoginHandler
    LogoutHandler securitycontract.LogoutHandler

    LoginName          string
    LoginAttributes    map[string]any
    LoginAuthenticator securitycontract.Authenticator
    LoginMethods       []string
    LoginTargetPath    string
    LoginCrossSite     CrossSitePolicy
    LoginRateLimit     *httpmiddleware.RateLimitGate

    LogoutName       string
    LogoutAttributes map[string]any
    LogoutMethods    []string
    LogoutTargetPath string
    LogoutCrossSite  CrossSitePolicy

    RoleHierarchySource         Source
    AccessDecisionManagerSource Source
    AccessControlSource         Source
    EntryPointSource            Source
    AccessDeniedHandlerSource   Source

    /* UnavailableRetryAfter is the Retry-After of the 503 an unavailable token earns on a protected route: absent is 5s, 1s to 1h in whole seconds, unlimited refused. */
    UnavailableRetryAfter bound.Duration
}

func NewCompiledFirewall(config CompiledFirewallConfig) *CompiledFirewall {
    if "" == config.Name {
        exception.Panic(exception.NewError("security firewall name may not be empty", nil, nil))
    }

    if true == internal.IsNilInterface(config.Matcher) {
        exception.Panic(
            exception.NewError(
                "security firewall matcher is nil",
                exceptioncontract.Context{
                    "firewallName": config.Name,
                },
                nil,
            ),
        )
    }

    /* the firewall's name is in hand here, so the refusal names it beside the index, as the builder and Compile do; NewFirewall alone knows only the index */
    for ruleIndex, rule := range config.Rules {
        if true == internal.IsNilInterface(rule) {
            exception.Panic(
                exception.NewError(
                    "security firewall rule is nil",
                    exceptioncontract.Context{
                        "firewallName": config.Name,
                        "ruleIndex":    ruleIndex,
                    },
                    nil,
                ),
            )
        }
    }

    unavailableRetryAfter, retryAfterErr := melodyconfig.ResolveUnavailableRetryAfter(config.UnavailableRetryAfter, "CompiledFirewallConfig.UnavailableRetryAfter")
    if nil != retryAfterErr {
        refusal := exception.FromError(retryAfterErr)
        refusal.SetContextValue("firewallName", config.Name)

        exception.Panic(refusal)
    }

    var firewall *Firewall
    if 0 != len(config.Rules) {
        firewall = NewFirewall(config.Rules)
    }

    return &CompiledFirewall{
        name:                        config.Name,
        matcher:                     config.Matcher,
        matcherDescription:          config.MatcherDescription,
        rules:                       append([]securitycontract.Rule{}, config.Rules...),
        firewall:                    firewall,
        tokenSource:                 config.TokenSource,
        accessControl:               config.AccessControl,
        accessDecisionManager:       config.AccessDecisionManager,
        roleHierarchy:               config.RoleHierarchy,
        entryPoint:                  config.EntryPoint,
        accessDeniedHandler:         config.AccessDeniedHandler,
        loginPath:                   config.LoginPath,
        logoutPath:                  config.LogoutPath,
        loginHandler:                config.LoginHandler,
        logoutHandler:               config.LogoutHandler,
        loginDoor: firewallDoorSettings{
            name:       config.LoginName,
            attributes: internal.CopyAnyMap(config.LoginAttributes),
            methods:    doorMethodsOrDefault(config.LoginMethods),
            targetPath: doorTargetPathOrDefault(config.LoginTargetPath),
            crossSite:  config.LoginCrossSite,
        },
        logoutDoor: firewallDoorSettings{
            name:       config.LogoutName,
            attributes: internal.CopyAnyMap(config.LogoutAttributes),
            methods:    doorMethodsOrDefault(config.LogoutMethods),
            targetPath: doorTargetPathOrDefault(config.LogoutTargetPath),
            crossSite:  config.LogoutCrossSite,
        },
        loginAuthenticator: config.LoginAuthenticator,
        loginRateLimit:     config.LoginRateLimit,
        roleHierarchySource:         sourceOrNone(config.RoleHierarchySource),
        accessDecisionManagerSource: sourceOrNone(config.AccessDecisionManagerSource),
        accessControlSource:         sourceOrNone(config.AccessControlSource),
        entryPointSource:            sourceOrNone(config.EntryPointSource),
        accessDeniedHandlerSource:   sourceOrNone(config.AccessDeniedHandlerSource),
        unavailableRetryAfter:       unavailableRetryAfter,
    }
}

func sourceOrNone(source Source) Source {
    if "" == source {
        return SourceNone
    }

    return source
}

type CompiledFirewall struct {
    name               string
    matcherDescription string
    loginPath          string
    logoutPath         string

    matcher               securitycontract.Matcher
    accessDecisionManager securitycontract.AccessDecisionManager
    tokenSource           securitycontract.TokenSource
    entryPoint            securitycontract.EntryPoint
    accessDeniedHandler   securitycontract.AccessDeniedHandler
    loginHandler          securitycontract.LoginHandler
    logoutHandler         securitycontract.LogoutHandler
    loginAuthenticator    securitycontract.Authenticator
    loginRateLimit        *httpmiddleware.RateLimitGate
    loginDoor             firewallDoorSettings
    logoutDoor            firewallDoorSettings

    rules                       []securitycontract.Rule
    firewall                    *Firewall
    accessControl               *AccessControl
    roleHierarchy               *RoleHierarchy
    roleHierarchySource         Source
    accessDecisionManagerSource Source
    accessControlSource         Source
    entryPointSource            Source
    accessDeniedHandlerSource   Source
    unavailableRetryAfter       time.Duration
}

func (instance *CompiledFirewall) Name() string {
    return instance.name
}

func (instance *CompiledFirewall) Matcher() securitycontract.Matcher {
    return instance.matcher
}

func (instance *CompiledFirewall) MatcherDescription() string {
    return instance.matcherDescription
}

func (instance *CompiledFirewall) Rules() []securitycontract.Rule {
    return append([]securitycontract.Rule{}, instance.rules...)
}

/* UnavailableRetryAfter answers the Retry-After delay of the 503 this firewall refuses an unavailable token with. */
func (instance *CompiledFirewall) UnavailableRetryAfter() time.Duration {
    return instance.unavailableRetryAfter
}

func (instance *CompiledFirewall) TokenSource() securitycontract.TokenSource {
    return instance.tokenSource
}

func (instance *CompiledFirewall) AccessControl() *AccessControl {
    return instance.accessControl
}

func (instance *CompiledFirewall) AccessDecisionManager() securitycontract.AccessDecisionManager {
    return instance.accessDecisionManager
}

func (instance *CompiledFirewall) RoleHierarchy() *RoleHierarchy {
    return instance.roleHierarchy
}

func (instance *CompiledFirewall) EntryPoint() securitycontract.EntryPoint {
    return instance.entryPoint
}

func (instance *CompiledFirewall) AccessDeniedHandler() securitycontract.AccessDeniedHandler {
    return instance.accessDeniedHandler
}

func (instance *CompiledFirewall) LoginPath() string {
    return instance.loginPath
}

func (instance *CompiledFirewall) LogoutPath() string {
    return instance.logoutPath
}

/* Login runs the firewall's login handler and leaves the session id as it was: the door rotates it first when it authenticated the credentials, and a handler behind a door with no authenticator that writes an identity into the session rotates the id itself, through http.RegenerateRequestSession, since only it knows when the privilege changes; a login that does not rotate is open to session fixation. A handler error raises security.login.failure only when it answers 401 or 403, a refused credential, and a failed dispatch of the event is filed at Error with the refusal kept. */
func (instance *CompiledFirewall) Login(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
    /* IsNilInterface: the handler comes through NewCompiledFirewall unvalidated, so a typed nil must not reach the call below */
    if true == internal.IsNilInterface(instance.loginHandler) {
        return nil, exception.NewError(
            "firewall login handler is nil",
            exceptioncontract.Context{
                "firewallName": instance.name,
            },
            nil,
        )
    }

    result, err := instance.loginHandler.Login(runtimeInstance, request, input)
    if nil != err {
        if false == raisesLoginFailure(err) {
            return nil, err
        }

        fileLoginFailureDispatchError(
            runtimeInstance,
            instance.dispatchLoginFailure(runtimeInstance, request, err),
            exception.AsHttpException(err).StatusCode(),
            exceptioncontract.Context{"firewallName": instance.name},
        )

        return nil, err
    }

    if nil == result {
        return nil, exception.NewError(
            "firewall login handler returned nil result",
            exceptioncontract.Context{
                "firewallName": instance.name,
            },
            nil,
        )
    }

    /* refused as the nil result is, so security.login.success never carries a token its listeners cannot read */
    if true == internal.IsNilInterface(result.Token) {
        return nil, exception.NewError(
            "firewall login handler returned nil token",
            exceptioncontract.Context{
                "firewallName": instance.name,
            },
            nil,
        )
    }

    dispatchErr := instance.dispatchLoginSuccess(runtimeInstance, request, result.Token)
    if nil != dispatchErr {
        http.CloseDiscardedResponseBody(result.Response, logging.LoggerFromRuntime(runtimeInstance))

        return nil, dispatchErr
    }

    return result, nil
}

/* Logout runs the firewall's logout handler. A result beside an error means the handler succeeded and security.logout.success could not be dispatched: the sign-out stands. */
func (instance *CompiledFirewall) Logout(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
    /* IsNilInterface, as for the login handler */
    if true == internal.IsNilInterface(instance.logoutHandler) {
        return nil, exception.NewError(
            "firewall logout handler is nil",
            exceptioncontract.Context{
                "firewallName": instance.name,
            },
            nil,
        )
    }

    result, err := instance.logoutHandler.Logout(runtimeInstance, request, input)
    if nil != err {
        dispatchErr := instance.dispatchLogoutFailure(runtimeInstance, request, err)
        if nil != dispatchErr {
            /* both failures travel as causes: the logout error first, so the client sees its reason, and the dispatch error beside it, so its context survives the render boundary */
            return nil, exception.NewError(
                "security logout failure event dispatch failed",
                exceptioncontract.Context{
                    "firewallName": instance.name,
                },
                errors.Join(err, dispatchErr),
            )
        }

        return nil, err
    }

    if nil == result {
        /* a nil result fails closed as in Login, since the door dereferences result.Response */
        return nil, exception.NewError(
            "firewall logout handler returned nil result",
            exceptioncontract.Context{
                "firewallName": instance.name,
            },
            nil,
        )
    }

    dispatchErr := instance.dispatchLogoutSuccess(runtimeInstance, request)
    if nil != dispatchErr {
        return result, dispatchErr
    }

    return result, nil
}

func (instance *CompiledFirewall) Sources() (Source, Source, Source, Source, Source) {
    return instance.roleHierarchySource, instance.accessDecisionManagerSource, instance.accessControlSource, instance.entryPointSource, instance.accessDeniedHandlerSource
}

func (instance *CompiledFirewall) dispatchLoginSuccess(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    token securitycontract.Token,
) error {
    eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Context(), runtimeInstance.Container())

    _, err := eventDispatcher.DispatchName(
        runtimeInstance,
        securitycontract.EventSecurityLoginSuccess,
        NewLoginSuccessEvent(request, token),
    )

    return err
}

func (instance *CompiledFirewall) dispatchLoginFailure(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    failureErr error,
) error {
    eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Context(), runtimeInstance.Container())

    _, err := eventDispatcher.DispatchName(
        runtimeInstance,
        securitycontract.EventSecurityLoginFailure,
        NewLoginFailureEvent(request, failureErr),
    )

    return err
}

func (instance *CompiledFirewall) dispatchLogoutSuccess(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) error {
    eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Context(), runtimeInstance.Container())

    _, err := eventDispatcher.DispatchName(
        runtimeInstance,
        securitycontract.EventSecurityLogoutSuccess,
        NewLogoutSuccessEvent(request),
    )

    return err
}

func (instance *CompiledFirewall) dispatchLogoutFailure(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    failureErr error,
) error {
    eventDispatcher := event.EventDispatcherMustFromContainer(runtimeInstance.Context(), runtimeInstance.Container())

    _, err := eventDispatcher.DispatchName(
        runtimeInstance,
        securitycontract.EventSecurityLogoutFailure,
        NewLogoutFailureEvent(request, failureErr),
    )

    return err
}

var _ securitycontract.Firewall = (*CompiledFirewall)(nil)

type CompiledConfiguration struct {
    firewalls []*CompiledFirewall

    globalAccessControl *AccessControl
}

func NewCompiledConfiguration(firewalls []*CompiledFirewall, globalAccessControl *AccessControl) *CompiledConfiguration {
    return &CompiledConfiguration{
        firewalls:           append([]*CompiledFirewall{}, firewalls...),
        globalAccessControl: globalAccessControl,
    }
}

func (instance *CompiledConfiguration) Firewalls() []*CompiledFirewall {
    return append([]*CompiledFirewall{}, instance.firewalls...)
}

func (instance *CompiledConfiguration) GlobalAccessControl() *AccessControl {
    return instance.globalAccessControl
}
