package security

import (
    "reflect"
    "sync/atomic"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* DefaultSwitchUserHeaderName is the request header that names the user to impersonate unless a source overrides it. */
const DefaultSwitchUserHeaderName = "X-Switch-User"

/* ImpersonationTokenSourceConfig configures the switch-user decorator. Inner resolves the real (admin) token first; when the switch header is present, the admin is authenticated and holds SwitchRole, Users resolves the target identity and the result impersonates it. An absent header, an anonymous admin or an admin without SwitchRole leaves the admin's own token untouched; a target that does not resolve, resolves unauthenticated or fails the resolver answers the anonymous token. */
type ImpersonationTokenSourceConfig struct {
    Inner securitycontract.TokenSource

    Users securitycontract.ImpersonatedUserResolver

    /* HeaderName overrides the switch-user header; defaults to DefaultSwitchUserHeaderName. */
    HeaderName string

    /* SwitchRole is the role the admin must hold to be allowed to switch; defaults to contract.RoleAllowedToSwitch. The role reaches every identity Users answers, an administrator's included, so a ceiling on whom a holder may become belongs in the resolver: it refuses the identities this admin may not take on. */
    SwitchRole string

    /* RoleMode selects whose roles the impersonation token authorizes with: RoleModeImpersonated (the default) takes on the target's roles for their full context, RoleModeImpersonator keeps the admin's own rights. The impersonator stays auditable and propagates between services in either mode. */
    RoleMode ImpersonationRoleMode

    /* RoleHierarchy, when set, expands the admin's roles before the switch-role check, as the access-decision path does; nil reads the hierarchy of the firewall holding the source, bound at Compile, and the raw token roles where the firewall has none or the source was compiled by hand. A source held by firewalls with different hierarchies is refused at Compile unless this field decides for it. */
    RoleHierarchy *RoleHierarchy
}

func NewImpersonationTokenSource(config ImpersonationTokenSourceConfig) *ImpersonationTokenSource {
    if true == internal.IsNilInterface(config.Inner) {
        exception.Panic(exception.NewError("impersonation inner token source is nil", nil, nil))
    }

    if true == internal.IsNilInterface(config.Users) {
        exception.Panic(exception.NewError("impersonation user resolver is nil", nil, nil))
    }

    headerName := config.HeaderName
    if "" == headerName {
        headerName = DefaultSwitchUserHeaderName
    }

    switchRole := config.SwitchRole
    if "" == switchRole {
        switchRole = securitycontract.RoleAllowedToSwitch
    }

    return &ImpersonationTokenSource{
        inner:         config.Inner,
        users:         config.Users,
        headerName:    headerName,
        switchRole:    switchRole,
        roleMode:      config.RoleMode,
        roleHierarchy: config.RoleHierarchy,
    }
}

type ImpersonationTokenSource struct {
    inner          securitycontract.TokenSource
    users          securitycontract.ImpersonatedUserResolver
    headerName     string
    switchRole     string
    roleMode       ImpersonationRoleMode
    roleHierarchy  *RoleHierarchy
    boundHierarchy atomic.Pointer[boundRoleHierarchy]
}

/* boundRoleHierarchy is what the first firewall holding the source handed it; hierarchy is nil when that firewall has none */
type boundRoleHierarchy struct {
    firewallName string
    hierarchy    securitycontract.RoleHierarchy
}

/* BindRoleHierarchy records the hierarchy of the firewall holding the source, which the switch-role check reads when RoleHierarchy is left nil, and answers an error when another firewall already bound a different one, a nil one included: the source holds one hierarchy, and the second would change who may switch behind the first firewall. An explicit RoleHierarchy records nothing. A binder Inner is handed the same call. */
func (instance *ImpersonationTokenSource) BindRoleHierarchy(firewallName string, hierarchy securitycontract.RoleHierarchy) error {
    if inner, isBinder := instance.inner.(securitycontract.RoleHierarchyBinder); true == isBinder {
        if innerErr := inner.BindRoleHierarchy(firewallName, hierarchy); nil != innerErr {
            return innerErr
        }
    }

    if nil != instance.roleHierarchy {
        return nil
    }

    if true == internal.IsNilInterface(hierarchy) {
        hierarchy = nil
    }

    candidate := &boundRoleHierarchy{firewallName: firewallName, hierarchy: hierarchy}
    if true == instance.boundHierarchy.CompareAndSwap(nil, candidate) {
        return nil
    }

    current := instance.boundHierarchy.Load()
    if true == isSameRoleHierarchy(current.hierarchy, hierarchy) {
        return nil
    }

    return exception.NewError(
        "the impersonation source is held by two firewalls with different role hierarchies; declare its RoleHierarchy",
        exceptioncontract.Context{
            "field":             "ImpersonationTokenSourceConfig.RoleHierarchy",
            "firewallName":      firewallName,
            "boundFirewallName": current.firewallName,
        },
        nil,
    )
}

func (instance *ImpersonationTokenSource) expandSwitchRoles(roles []string) []string {
    if nil != instance.roleHierarchy {
        return instance.roleHierarchy.ExpandRoles(roles)
    }

    if current := instance.boundHierarchy.Load(); nil != current && nil != current.hierarchy {
        return current.hierarchy.ExpandRoles(roles)
    }

    return roles
}

/* isSameRoleHierarchy compares by identity; a hierarchy that cannot be compared is different, since == on it would panic */
func isSameRoleHierarchy(first securitycontract.RoleHierarchy, second securitycontract.RoleHierarchy) bool {
    if nil == first || nil == second {
        return nil == first && nil == second
    }

    if false == reflect.ValueOf(first).Comparable() || false == reflect.ValueOf(second).Comparable() {
        return false
    }

    return first == second
}

func (instance *ImpersonationTokenSource) Name() string {
    return instance.inner.Name()
}

func (instance *ImpersonationTokenSource) Resolve(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (securitycontract.Token, error) {
    innerToken, resolveErr := instance.inner.Resolve(runtimeInstance, request)
    if nil != resolveErr {
        return nil, resolveErr
    }

    target := request.Header(instance.headerName)
    if "" == target {
        return innerToken, nil
    }

    if true == internal.IsNilInterface(innerToken) || false == innerToken.IsAuthenticated() {
        instance.logDenied(runtimeInstance, "anonymous principal can not switch user", target)

        return innerToken, nil
    }

    if false == hasRole(instance.expandSwitchRoles(innerToken.Roles()), instance.switchRole) {
        instance.logDenied(runtimeInstance, "principal is not allowed to switch user", target)

        return innerToken, nil
    }

    impersonatedToken, userErr := instance.users.ResolveImpersonatedUser(runtimeInstance, target)
    if nil != userErr {
        resolutionErr := exception.NewError(
            "could not resolve the impersonated user",
            map[string]any{"target": internal.BoundDiagnosticText(target)},
            userErr,
        )

        /* the caller passed the switch-role check, so a failed resolution never runs with the caller's broader roles: a resolver that cannot answer is the unavailable token, recorded once by the resolution listener, any other error a denial with an anonymous token; the earlier branches narrow nothing and pass the inner token through */
        if true == exception.IsInfrastructureFailure(userErr) {
            return NewUnavailableToken(resolutionErr), nil
        }

        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Info("switch-user denied", exception.LogContext(resolutionErr, exception.LogContextConfig{}))
        }

        return NewAnonymousToken(), nil
    }

    if true == internal.IsNilInterface(impersonatedToken) || false == impersonatedToken.IsAuthenticated() {
        instance.logDenied(runtimeInstance, "impersonated user is unknown or not authenticated", target)

        return NewAnonymousToken(), nil
    }

    return NewImpersonationToken(impersonatedToken, innerToken, instance.roleMode), nil
}

func (instance *ImpersonationTokenSource) logDenied(
    runtimeInstance runtimecontract.Runtime,
    message string,
    target string,
) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil != logger {
        logger.Info(
            "switch-user denied",
            exception.LogContext(exception.NewError(message, map[string]any{"target": internal.BoundDiagnosticText(target)}, nil), exception.LogContextConfig{}),
        )
    }
}

var _ securitycontract.TokenSource = (*ImpersonationTokenSource)(nil)
var _ securitycontract.RoleHierarchyBinder = (*ImpersonationTokenSource)(nil)
