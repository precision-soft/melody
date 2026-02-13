# SECURITY

The [`security`](../../security) package provides Melody’s HTTP security building blocks: firewall matching, request authentication, access control (path-based attributes), and role-based authorization.

## Scope

- Package: `security/`
- Subpackages:
    - `security/contract/`
    - `security/config/`

## Subpackages

- [`security/contract`](../../security/contract)  
  Public contracts for firewalls, token sources, access decisions, and handlers.

- [`security/config`](../../security/config)  
  Builder-based configuration and compilation into a runtime-ready `security.CompiledConfiguration`.

## Responsibilities

- Model authentication state via `Token` implementations (`AuthenticatedToken`, `AnonymousToken`).
- Define `Firewall` evaluation by applying `Rule` checks and resolving a request `Token` via a `TokenSource`.
- Provide path-based access control rules (`AccessControlRule`) with deterministic match priority.
- Provide role/attribute authorization via `AccessDecisionManager` and `Voter` implementations.
- Provide event types and standard kernel listeners for:
    - security context resolution (`RegisterKernelSecurityResolutionListener`)
    - access control enforcement (`RegisterKernelAccessControlListener`)

## Configuration

### Compiled configuration

Security is wired through a compiled configuration:

- [`security.CompiledConfiguration`](../../security/compiled_configuration.go)
- [`security.NewCompiledConfiguration`](../../security/compiled_configuration.go)

The `security/config` subpackage provides the user-facing builder and the compilation entry point:

- [`securityconfig.NewBuilder`](../../security/config/security_module.go)
- [`securityconfig.Builder.BuildAndCompile`](../../security/config/security_module.go)
- [`securityconfig.Compile`](../../security/config/compile.go)

### Access control merge strategies

When a firewall defines both global and local access control, `security/config` merges them according to:

- [`securityconfig.AccessControlMergeStrategy`](../../security/config/security_module.go)
    - `localFirst`
    - `globalFirst`
    - `overrideOnly`

## Container integration

The package defines the firewall manager service id:

- [`security.ServiceFirewallManager`](../../security/service_resolver.go) (`"service.security.firewallManager"`)

Resolution helpers:

- [`FirewallManagerMustFromContainer`](../../security/service_resolver.go)
- [`FirewallManagerFromContainer`](../../security/service_resolver.go)

## Match and authorization semantics

### Security context resolution

If a request matches a configured firewall, Melody always stores a security context in the runtime, even when token resolution fails.

- Kernel listener: [`security.RegisterKernelSecurityResolutionListener`](../../security/security_resolution_listener.go)
- Context type: [`security.SecurityContext`](../../security/security_context.go)
- Token contract: [`securitycontract.Token`](../../security/contract/token.go)
- Token source contract: [`securitycontract.TokenSource`](../../security/contract/token_source.go)

Token resolution outcomes:

- **Authenticated token** when the resolved token returns `true == token.IsAuthenticated()` (for example [`security.AuthenticatedToken`](../../security/authenticated_token.go)).
- **Anonymous token** when resolution returns `nil`, returns an error, or panics (see [`security.AnonymousToken`](../../security/anonymous_token.go)).

Userland code must treat `token.IsAuthenticated()` as the canonical guard for accessing user identity or enforcing roles (or use [`security.IsGranted`](../../security/is_granted.go)).

### Access control matching

`AccessControl.Match(path)` selects attributes based on the following priority:

1. **Exact match** (`NewAccessControlExactRule`)
2. **Prefix match** (`NewAccessControlRule` / `NewAccessControlRuleWithSegmentPrefix`) with **longest prefix wins**
3. **Regex match** (`NewAccessControlRegexRule`) with **first match wins** (declaration order)
4. **Fallback** rule with an empty prefix (if present)

This ordering is validated by tests in [`security/access_control_test.go`](../../security/access_control_test.go).

### Role checks

`IsGranted(runtimeInstance, role)` checks for a resolved `SecurityContext` token in the runtime and returns whether the token has the requested role.

- [`IsGranted`](../../security/is_granted.go)

## Usage

The example below demonstrates a typical Melody application flow:

- a module contributes security configuration via `application.SecurityModule`;
- a single firewall matches all `/admin` requests;
- authentication is resolved from an API key header;
- access control requires `ROLE_ADMIN` for `/admin` and allows anonymous access to `/`.

```go
package example

import (
	"errors"

	applicationcontract "github.com/precision-soft/melody/application/contract"
	httpcontract "github.com/precision-soft/melody/http/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	"github.com/precision-soft/melody/security"
	securityconfig "github.com/precision-soft/melody/security/config"
	securitycontract "github.com/precision-soft/melody/security/contract"
)

type apiKeyLoginHandler struct{}

func (instance *apiKeyLoginHandler) Login(
	runtimeInstance any,
	request httpcontract.Request,
	input securitycontract.LoginInput,
) (*securitycontract.LoginResult, error) {
	_ = runtimeInstance
	_ = request

	if nil == input.Token {
		return nil, errors.New("token is required")
	}

	return &securitycontract.LoginResult{
		Token:    input.Token,
		Response: nil,
	}, nil
}

type apiKeyLogoutHandler struct{}

func (instance *apiKeyLogoutHandler) Logout(
	runtimeInstance any,
	request httpcontract.Request,
	input securitycontract.LogoutInput,
) (*securitycontract.LogoutResult, error) {
	_ = runtimeInstance
	_ = request
	_ = input

	return &securitycontract.LogoutResult{Response: nil}, nil
}

type adminSecurityModule struct{}

func (instance *adminSecurityModule) Name() string {
	return "example.security"
}

func (instance *adminSecurityModule) Description() string {
	return "example security module"
}

func (instance *adminSecurityModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
	_ = kernelInstance
}

func (instance *adminSecurityModule) RegisterSecurity(builder *securityconfig.Builder) {
	matcher := security.NewPathPrefixMatcher("/admin")

	authenticator := security.NewApiKeyHeaderAuthenticator(
		"X-Api-Key",
		"secret",
		"system",
		[]string{"ROLE_ADMIN"},
	)

	authenticatorManager := security.NewAuthenticatorManager(
		authenticator,
	)

	tokenSource := security.NewAuthenticatorTokenSource(
		authenticatorManager,
	)

	accessControl := securityconfig.NewAccessControlBuilder().
		Require("/admin", "ROLE_ADMIN").
		AllowAnonymous("/").
		Build()

	roleVoter := security.NewRoleVoter()
	accessDecisionManager := security.NewAccessDecisionManager(
		securitycontract.DecisionStrategyAffirmative,
		roleVoter,
	)

	builder.SetGlobal(
		accessControl,
		nil,
		accessDecisionManager,
		nil,
		nil,
	)

	builder.AddFirewall(
		"admin",
		matcher,
		[]securitycontract.Rule{},
		tokenSource,
		"/login",
		"/logout",
		&apiKeyLoginHandler{},
		&apiKeyLogoutHandler{},
		securityconfig.NewFirewallOverrideConfiguration(),
	)

	builder.AddStatelessFirewall(
		"admin",
		matcher,
		[]securitycontract.Rule{},
		tokenSource,
		securityconfig.NewFirewallOverrideConfiguration(),
	)

	builder.AddFirewall(
		"admin",
		matcher,
		[]securitycontract.Rule{},
		tokenSource,
		"",
		"",
		nil,
		nil,
		securityconfig.NewFirewallOverrideConfiguration().WithStateless(true),
	)
}

var _ applicationcontract.HttpModule = (*adminSecurityModule)(nil)
```

## Footguns & caveats

- `AccessControl` uses a deterministic match priority: exact match first, then longest prefix match (including segment-prefix rules), then regex rules in the order they were registered, then the empty-prefix fallback. See [`(*AccessControl).Match`](../../security/access_control.go).
- `SecurityContextSetOnRuntime` stores the context in the runtime scope under `security/contract.ServiceSecurityContext`.

## Userland API

### Contracts (`security/contract`)

#### Types

- [`Rule`](../../security/contract/rule.go)
- [`Matcher`](../../security/contract/matcher.go)
- [`Token`](../../security/contract/token.go)
- [`TokenSource`](../../security/contract/token_source.go)
- [`Authenticator`](../../security/contract/authenticator.go)
- [`Firewall`](../../security/contract/firewall.go)
- [`FirewallManager`](../../security/contract/firewall_manager.go)
- [`AccessDecisionManager`](../../security/contract/access_decision_manager.go)
- [`Voter`](../../security/contract/voter.go)
- [`EntryPoint`](../../security/contract/entry_point.go)
- [`AccessDeniedHandler`](../../security/contract/access_denied_handler.go)
- [`LoginHandler`](../../security/contract/login_handler.go)
- [`LogoutHandler`](../../security/contract/logout_handler.go)

#### Constants

- [`ServiceSecurityContext`](../../security/contract/const.go)
- Events: [`EventSecurityAuthorizationGranted`, `EventSecurityAuthorizationDenied`, `EventSecurityLoginSuccess`, `EventSecurityLoginFailure`, `EventSecurityLogoutSuccess`, `EventSecurityLogoutFailure`](../../security/contract/const.go)
- [`AttributePublicAccess`](../../security/contract/const.go)

### Types

- [`AccessControl`](../../security/access_control.go)
- [`AccessControlRule`](../../security/access_control.go)
- [`RoleHierarchy`](../../security/role_hierarchy.go)
- Tokens: [`AnonymousToken`](../../security/anonymous_token.go), [`AuthenticatedToken`](../../security/authenticated_token.go), [`Token`](../../security/token.go)
- Auth: [`ApiKeyHeaderRule`](../../security/rule.go), [`ApiKeyHeaderAuthenticator`](../../security/api_key_authenticator.go), [`AuthenticatorManager`](../../security/authenticator_manager.go), [`AuthenticatorTokenSource`](../../security/token_source.go)
- Matchers: [`PathPrefixMatcher`](../../security/matcher.go)
- Authorization: [`AccessDecisionManager`, `RoleVoter`](../../security/voter.go)
- Configuration: [`CompiledConfiguration`, `CompiledFirewall`, `CompiledSource`](../../security/compiled_configuration.go)
- Context: [`SecurityContext`](../../security/security_context.go)

### Constructors

- [`NewAccessControl(rules...)`](../../security/access_control.go)
- [`NewAccessControlRule(pathPrefix string, attributes ...string)`](../../security/access_control.go)
- [`NewAccessControlExactRule(path string, attributes ...string)`](../../security/access_control.go)
- [`NewAccessControlRegexRule(pattern string, attributes ...string)`](../../security/access_control.go)
- [`NewAccessControlRuleWithSegmentPrefix(pathPrefix string, attributes ...string)`](../../security/access_control.go)
- [`NewRoleHierarchy(hierarchy map[string][]string)`](../../security/role_hierarchy.go)
- [`NewAnonymousToken()`](../../security/anonymous_token.go)
- [`NewAuthenticatedToken(userIdentifier string, roles []string)`](../../security/authenticated_token.go)
- [`NewToken(user securitycontract.Token)`](../../security/token.go)
- [`NewPathPrefixMatcher(pathPrefix string)`](../../security/matcher.go)
- [`NewApiKeyHeaderRule(matcher securitycontract.Matcher, headerName string, expectedValue string)`](../../security/rule.go)
- [`NewApiKeyHeaderAuthenticator(headerName string, expectedValue string, userId string, roles []string)`](../../security/api_key_authenticator.go)
- [`NewAuthenticatorManager(authenticators ...securitycontract.Authenticator)`](../../security/authenticator_manager.go)
- [`NewAuthenticatorTokenSource(authenticatorManager *AuthenticatorManager)`](../../security/token_source.go)
- [`NewAccessDecisionManager(strategy securitycontract.DecisionStrategy, voters ...securitycontract.Voter)`](../../security/access_decision_manager.go)
- [`NewRoleVoter()`](../../security/voter.go)
- [`NewSecurityContext(firewall *CompiledFirewall, token securitycontract.Token)`](../../security/security_context.go)
- [`NewCompiledFirewall(...)`](../../security/compiled_configuration.go)
- [`NewCompiledConfiguration(...)`](../../security/compiled_configuration.go)

### Kernel listeners

- [`RegisterKernelSecurityResolutionListener(kernelcontract.Kernel, *FirewallRegistry)`](../../security/security_resolution_listener.go)
- [`RegisterKernelAccessControlListener(kernelcontract.Kernel, *FirewallRegistry)`](../../security/access_control_listener.go)

### Container and runtime helpers

- [`const ServiceFirewallManager`](../../security/service_resolver.go)
- [`FirewallManagerMustFromContainer(containercontract.Container)`](../../security/service_resolver.go)
- [`FirewallManagerFromContainer(containercontract.Container)`](../../security/service_resolver.go)
- [`SecurityContextSetOnRuntime(runtimecontract.Runtime, *SecurityContext)`](../../security/service_resolver.go)
- [`SecurityContextFromRuntime(runtimecontract.Runtime)`](../../security/service_resolver.go)

### Configuration (`security/config`)

- Builder: [`NewBuilder()` / `(*Builder).SetGlobal(...)` / `(*Builder).AddFirewall(...)` / `(*Builder).BuildAndCompile()`](../../security/config/security_module.go)
- Access control builder: [`NewAccessControlBuilder()` / `(*AccessControlBuilder).Require(...)` / `(*AccessControlBuilder).AllowAnonymous(...)` / `(*AccessControlBuilder).Build()`](../../security/config/access_control_builder.go)
- Compile: [`Compile(configuration)`](../../security/config/compile.go)
