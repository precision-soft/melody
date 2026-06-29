package security

import (
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* DefaultSwitchUserHeaderName is the request header that names the user to impersonate unless a source overrides it. */
const DefaultSwitchUserHeaderName = "X-Switch-User"

/* ImpersonationTokenSourceConfig configures the switch-user decorator. Inner resolves the real (admin) token first; when the switch header is present, the admin is authenticated and holds SwitchRole, Users resolves the target identity and the result impersonates it. Any missing precondition leaves the admin's own token untouched. */
type ImpersonationTokenSourceConfig struct {
    Inner securitycontract.TokenSource

    Users securitycontract.ImpersonatedUserResolver

    /* HeaderName overrides the switch-user header; defaults to DefaultSwitchUserHeaderName. */
    HeaderName string

    /* SwitchRole is the role the admin must hold to be allowed to switch; defaults to contract.RoleAllowedToSwitch. */
    SwitchRole string

    /* RoleMode selects whose roles the impersonation token authorizes with: RoleModeImpersonated (the default) takes on the target's roles for their full context, RoleModeImpersonator keeps the admin's own rights. The impersonator stays auditable and propagates between services in either mode. */
    RoleMode ImpersonationRoleMode
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
        inner:      config.Inner,
        users:      config.Users,
        headerName: headerName,
        switchRole: switchRole,
        roleMode:   config.RoleMode,
    }
}

type ImpersonationTokenSource struct {
    inner      securitycontract.TokenSource
    users      securitycontract.ImpersonatedUserResolver
    headerName string
    switchRole string
    roleMode   ImpersonationRoleMode
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

    if false == hasRole(innerToken.Roles(), instance.switchRole) {
        instance.logDenied(runtimeInstance, "principal is not allowed to switch user", target)

        return innerToken, nil
    }

    impersonatedToken, userErr := instance.users.ResolveImpersonatedUser(runtimeInstance, target)
    if nil != userErr {
        instance.logDenied(runtimeInstance, "could not resolve the impersonated user", target)

        return innerToken, nil
    }

    if true == internal.IsNilInterface(impersonatedToken) || false == impersonatedToken.IsAuthenticated() {
        instance.logDenied(runtimeInstance, "impersonated user is unknown or not authenticated", target)

        return innerToken, nil
    }

    return NewImpersonationTokenWithRoleMode(impersonatedToken, innerToken, instance.roleMode), nil
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
            exception.LogContext(exception.NewError(message, map[string]any{"target": target}, nil)),
        )
    }
}

var _ securitycontract.TokenSource = (*ImpersonationTokenSource)(nil)
