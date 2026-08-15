# SECURITY

The [`security`](../../security) package provides Melody’s HTTP security building blocks: firewall matching, request authentication, access control (path-based attributes), and role-based authorization.

## Scope

- Package: [`security/`](../../security)
- Subpackages:
    - [`security/contract/`](../../security/contract)
    - [`security/config/`](../../security/config)

## Subpackages

- [`security/contract`](../../security/contract)  
  Public contracts for firewalls, token sources, access decisions, and handlers.

- [`security/config`](../../security/config)  
  Builder-based configuration and compilation into a runtime-ready `security.CompiledConfiguration`.

## Responsibilities

- Model authentication state via `Token` implementations (`AuthenticatedToken`, `AnonymousToken`).
- Define `Firewall` evaluation by applying `Rule` checks and resolving a request `Token` via a `TokenSource`.
- Provide path-based access control rules (`AccessControlRule`) with deterministic match priority.
- Provide role/attribute authorization via `AccessDecisionManager` and `Voter` implementations. A substituted manager receives the declared role hierarchy through the `RoleHierarchyAware` capability, and is refused at compilation if it cannot apply one.
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
- [`securityconfig.Builder.BuildAndCompile`](../../security/config/security_module.go) — the public path from a declaration to the runtime
- [`securityconfig.Compile`](../../security/config/compile.go) — `Compile(configuration Configuration) (*security.CompiledConfiguration, error)`, the step `BuildAndCompile` performs. Its argument carries only unexported fields and has no constructor, and the builder never hands one out, so a caller outside the package can only pass the empty value: the answer is a nil compiled configuration and a nil error, which is what "no security was declared" looks like everywhere else.

### Access control merge strategies

When a firewall inherits the global access control, `security/config` merges the global and local rule lists in
the order the strategy names. The strategy orders the LIST; the matcher still resolves by category first, so an
earlier position only decides what the categories leave tied:

- [`securityconfig.AccessControlMergeStrategy`](../../security/config/security_module.go)
    - `localFirst` — the default
    - `globalFirst`
    - `overrideOnly` — the global policy is cut off entirely and nothing is merged

The strategy is chosen with
[`(FirewallOverrideConfiguration).WithMergeStrategy`](../../security/config/security_module.go), and inheritance
is turned off entirely with
[`(FirewallOverrideConfiguration).WithInheritGlobalAccessControl`](../../security/config/security_module.go). A
value that is none of the three is refused at the setter with a named panic.

An override built from `NewFirewallOverrideConfiguration()` — or from the zero value, which every setter reads as
the constructor — inherits the global policy under `localFirst`. A firewall that inherits nothing and declares no
access control of its own **enforces nothing behind it**: the compiled control is empty, no rule matches, and
every request reaches its handler. Pair `WithInheritGlobalAccessControl(false)` with `WithAccessControl`.

## Container integration

The package defines the firewall manager service id:

- [`security.ServiceFirewallManager`](../../security/service_resolver.go) (`"service.security.firewall_manager"`)

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
- **Anonymous token** when resolution returns `nil` (see [`security.AnonymousToken`](../../security/anonymous_token.go)) — the request then continues anonymously. A resolution that returns an **error** or **panics** also stores the anonymous context, but the request is terminated on the spot through the `kernel.exception` dispatch and never reaches access control or the handler.

Userland code must treat `token.IsAuthenticated()` as the canonical guard for accessing user identity or enforcing roles (or use [`security.IsGranted`](../../security/is_granted.go)).

### Access control matching

`AccessControl.Match(path)` selects attributes based on the following priority:

1. **Exact match** (`NewAccessControlExactRule`)
2. **Prefix match** (`NewAccessControlRule` / `NewAccessControlRuleWithSegmentPrefix`) with **longest prefix wins**
3. **Regex match** (`NewAccessControlRegexRule`) with **first match wins** (declaration order). The pattern is **unanchored** — it matches as a substring of the canonicalized path, so `"/public"` also matches `/admin/public-notes`. Anchor a rule that names one section: `"^/public(/|$)"`. This is the opposite of a route requirement, which melody anchors for you.
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
package main

import (
	"errors"

	applicationcontract "github.com/precision-soft/melody/application/contract"
	httpcontract "github.com/precision-soft/melody/http/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/precision-soft/melody/security"
	securityconfig "github.com/precision-soft/melody/security/config"
	securitycontract "github.com/precision-soft/melody/security/contract"
)

type apiKeyLoginHandler struct{}

func (instance *apiKeyLoginHandler) Login(
	runtimeInstance runtimecontract.Runtime,
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
	runtimeInstance runtimecontract.Runtime,
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

### Session fixation

An application that carries its authenticated identity in the session (rather than in a stateless Bearer token) must **rotate the session id on any privilege change, above all at login**. Otherwise an attacker who can plant a known session id in the victim's browser before authentication — through a fixation vector such as a link, a subdomain-scoped cookie, or an XSS write — holds a cookie that becomes fully authenticated the moment the victim logs in.

[`session.Manager.RegenerateSession`](../../session/manager.go) is the defence: it mints a fresh id, carries the current values over, and removes the storage entry the previous id pointed at, so the planted id no longer resolves to anything. Call it in the login handler **before** writing the identity into the session, and republish the returned session on [`RequestAttributeSession`](../../http/request.go) — the response path re-reads that attribute to decide what to save, so a rotation that is not republished writes the old values back under the old id and does not happen at all.

```go
rotated, rotateErr := sessionManager.RegenerateSession(sessionInstance)
if nil != rotateErr {
    return nil, rotateErr
}

request.Attributes().Set(melodyhttp.RequestAttributeSession, rotated)

/* sessionKeyUserId is application-owned; the framework defines no session key for the identity */
rotated.Set(sessionKeyUserId, user.Id())
```

Rotate on the way out too: logout should clear the session ([`Session.Clear`](../../session/session.go)), which deletes the stored entry and expires the browser cookie. See [SESSION](SESSION.md#rotating-the-session-id) for the full contract and its footguns, and the [session cookie](HTTP.md#session-cookie) section for the `Secure`/`SameSite` attributes that keep the rotated cookie from leaking in the first place.

## Footguns & caveats

- `AccessControl` uses a deterministic match priority: exact match first, then longest prefix match (including segment-prefix rules), then regex rules in the order they were registered, then the empty-prefix fallback. See [`(*AccessControl).Match`](../../security/access_control.go).
- A session-backed login that does not call [`RegenerateSession`](../../session/manager.go) is vulnerable to **session fixation**: the id the victim arrived with stays valid and authenticated. Rotating is a per-application responsibility — the framework cannot do it for you, because only the login handler knows when the privilege change happens. See [Session fixation](#session-fixation).
- `SecurityContextSetOnRuntime` stores the context in the runtime scope under `security/contract.ServiceSecurityContext`.
- [`ApiKeyHeaderAuthenticator`](../../security/api_key_authenticator.go) compares the supplied header against the expected value with [`crypto/subtle.ConstantTimeCompare`](https://pkg.go.dev/crypto/subtle#ConstantTimeCompare) so timing differences do not leak the expected key.
- Every refusal the decision manager produces carries the branch that produced it, in the exception context the response never renders: `reason` — one of the exported `RefusalReason*` constants — beside `strategy` and, where a single attribute was being weighed, `attribute`. The access control listener files one record per refusal naming the reason, the firewall and the matched rule, at warning; the one exception is `RefusalReasonNoVoterSupportsAttribute`, filed at **error**, because a firewall naming an attribute no configured voter looks at is a wiring fault answered fail-closed and nothing about the request can repair it. The record is filed on whichever exit the path takes — a handler that answers the request itself no longer hides the refusal, and the record says what the handler did with it — and the error carried into `kernel.exception` is marked as already logged so the exception listener attaches its coordinates instead of filing a second record.
- Both methods of the decision manager refuse an empty attribute list. `DecideAll` used to read it as an AND over nothing and grant, so a call site whose attributes came from a configuration value that resolved away was authorized rather than refused; it now answers `403` exactly as `DecideAny` always did. The compiled access control cannot produce a rule with no attribute, so this only ever affected a caller reaching the decision manager directly.
- A typed nil — an interface variable holding a nil pointer — is refused wherever a nil is refused, both at the definition site and in `Compile`. Such a value is not equal to nil, so before this it survived both walls and was first dereferenced on the request path, outside any recovery. Declaring a dependency as `var handler *MyLoginHandler` and never assigning it now fails the boot with the piece named, rather than the first request that reaches it.
- A constructor that takes a slice copies it. Keeping your own reference to the rules, authenticators or voters you registered and editing it afterwards changes nothing about the firewall that was built from it — the swap would otherwise land past every nil check and past compilation, in the decision path of a live request.
- **`AccessControl` and `RoleHierarchy` are concrete types, not contracts, and that is deliberate.** Everything else a firewall is assembled from — `Matcher`, `Rule`, `Voter`, `EntryPoint`, `AccessDeniedHandler`, `TokenSource`, `LoginHandler`, `LogoutHandler` — is an interface an integrator can implement, so the absence here is a decision rather than an omission. These two own the two things the rest of the package is written against: the **match priority** documented above (exact, then longest prefix, then regex in registration order, then the empty-prefix fallback) and the **expansion rule** the role voters and `IsGranted` both read. A substituted matcher that ordered rules differently would silently change which rule answers a path, and this is the one place in melody where "silently changes which rule answers" means an authorization decision — the paragraph on the match priority would stop being true of the running application while still being the only thing anyone had read. Bring your own policy through a `Voter` instead: it is consulted for every attribute, it composes with the strategies, and it cannot reorder what it did not build. `RoleHierarchy` reaches a foreign decision manager through the [`RoleHierarchyAware`](../../security/access_decision_manager.go) capability, and a foreign voter through [`NewRoleHierarchyVoter`](../../security/role_hierarchy_voter.go), so the expansion is available without the type being replaceable.

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
- Auth: [`ApiKeyHeaderRule`](../../security/rule.go), [`ApiKeyHeaderAuthenticator`](../../security/api_key_authenticator.go), [`AuthenticatorManager`](../../security/authenticator_manager.go), [`AuthenticatorTokenSource`](../../security/token_source.go), [`ResolverTokenSource`](../../security/token_source.go)
- Matchers: [`PathPrefixMatcher`](../../security/matcher.go)
- Authorization: [`AccessDecisionManager`](../../security/access_decision_manager.go), [`RoleVoter`](../../security/voter.go), [`RoleHierarchyVoter`](../../security/role_hierarchy_voter.go)
- Configuration: [`CompiledConfiguration`, `CompiledFirewall`](../../security/compiled_configuration.go), [`Source`](../../security/security_context.go), [`FirewallRegistry`](../../security/firewall_registry.go), [`FirewallManager`](../../security/firewall_manager.go)
- Context: [`SecurityContext`](../../security/security_context.go)

### Constructors

- [`NewAccessControl(rules...)`](../../security/access_control.go)
- [`NewAccessControlRule(pathPrefix string, attributes ...string)`](../../security/access_control.go)
- [`NewAccessControlExactRule(path string, attributes ...string)`](../../security/access_control.go)
- [`NewAccessControlRegexRule(pattern string, attributes ...string)`](../../security/access_control.go)
- [`NewAccessControlRuleWithSegmentPrefix(pathPrefix string, attributes ...string)`](../../security/access_control.go)
- [`NewRoleHierarchy(inheritedRolesByRole map[string][]string)`](../../security/role_hierarchy.go)
- [`NewAnonymousToken()`](../../security/anonymous_token.go)
- [`NewAuthenticatedToken(userIdentifier string, roles []string)`](../../security/authenticated_token.go)
- [`NewToken(user securitycontract.Token)`](../../security/token.go)
- [`NewPathPrefixMatcher(prefix string)`](../../security/matcher.go)
- [`NewApiKeyHeaderRule(matcher securitycontract.Matcher, headerName string, expectedValue string)`](../../security/rule.go)
- [`NewApiKeyHeaderAuthenticator(headerName string, expectedValue string, userId string, roles []string)`](../../security/api_key_authenticator.go)
- [`NewAuthenticatorManager(authenticators ...securitycontract.Authenticator)`](../../security/authenticator_manager.go)
- [`NewAuthenticatorTokenSource(manager *AuthenticatorManager)`](../../security/token_source.go)
- [`NewResolverTokenSource(resolver securitycontract.TokenResolver)`](../../security/token_source.go)
- [`NewAccessDecisionManager(strategy securitycontract.DecisionStrategy, voters ...securitycontract.Voter)`](../../security/access_decision_manager.go)
- [`NewAccessDecisionManagerWithVoters(strategy securitycontract.DecisionStrategy, voters []securitycontract.Voter)`](../../security/access_decision_manager.go)
- [`type RoleHierarchyAware`](../../security/access_decision_manager.go) — the optional capability an `AccessDecisionManager` implements to receive the declared role hierarchy at compilation, answering the manager that applies it. The built-in manager implements it by wrapping its own `RoleVoter`s; a manager that does not implement it and is handed a hierarchy is **refused at compilation, by firewall name**, because the alternative was silence: the assertion the compilation used to make on the concrete type let a foreign manager — even a delegating wrapper — skip the upgrade, so `ROLE_ADMIN: [ROLE_USER]` stopped applying on the enforcement path while `IsGranted`, which expands the hierarchy straight from the compiled firewall, kept answering for it
- [`(*AccessDecisionManager).WithRoleHierarchy(roleHierarchy *RoleHierarchy)`](../../security/access_decision_manager.go) — answers a manager whose built-in role voters read the expanded roles, leaving every other voter as it was; a nil hierarchy answers the manager unchanged
- [`RefusalReasonEmptyAttributeList`, `RefusalReasonNoAttributeGranted`, `RefusalReasonAllVotersAbstained`, `RefusalReasonNoVoterSupportsAttribute`, `RefusalReasonAffirmativeNoGrant`, `RefusalReasonConsensusDenied`, `RefusalReasonConsensusTie`, `RefusalReasonUnanimousDenied`, `RefusalReasonUnanimousNoGrant`](../../security/access_decision_manager.go) — the branch a `403` names in its context
- [`NewRoleVoter()`](../../security/voter.go)
- [`NewRoleHierarchyVoter(roleHierarchy *RoleHierarchy, delegate securitycontract.Voter)`](../../security/role_hierarchy_voter.go) — the delegate is any `Voter`, so an integrator's own voter can be handed the expanded roles instead of reimplementing the expansion rule
- [`NewSecurityContext(firewall *CompiledFirewall, token securitycontract.Token)`](../../security/security_context.go)
- [`NewFirewall(rules ...securitycontract.Rule)`](../../security/firewall.go)
- [`NewFirewallManager(compiledConfiguration *CompiledConfiguration)`](../../security/firewall_manager.go)
- [`NewFirewallRegistry(compiledConfiguration *CompiledConfiguration)`](../../security/firewall_registry.go)
- [`NewCompiledFirewall(...)`](../../security/compiled_configuration.go)
- [`NewCompiledConfiguration(firewalls []*CompiledFirewall, globalAccessControl *AccessControl)`](../../security/compiled_configuration.go)

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

- Builder:
    - [`NewBuilder()`](../../security/config/security_module.go)
    - [`(*Builder).SetGlobal(accessControl, roleHierarchy, accessDecisionManager, entryPoint, accessDeniedHandler)`](../../security/config/security_module.go)
    - [`(*Builder).AddFirewall(name, matcher, rules, tokenSource, loginPath, logoutPath, loginHandler, logoutHandler, override)`](../../security/config/security_module.go)
    - [`(*Builder).AddStatefulFirewall(name, matcher, rules, tokenSource, loginPath, logoutPath, loginHandler, logoutHandler, override)`](../../security/config/security_module.go)
    - [`(*Builder).AddStatelessFirewall(name, matcher, rules, tokenSource, override)`](../../security/config/security_module.go)
    - [`(*Builder).BuildAndCompile() *security.CompiledConfiguration`](../../security/config/security_module.go)
- Firewall overrides:
    - [`type FirewallOverrideConfiguration`](../../security/config/security_module.go)
    - [`NewFirewallOverrideConfiguration()`](../../security/config/security_module.go)
    - [`(FirewallOverrideConfiguration).WithStateless(stateless bool) FirewallOverrideConfiguration`](../../security/config/security_module.go)
    - [`(FirewallOverrideConfiguration).WithAccessControl(accessControl *security.AccessControl) FirewallOverrideConfiguration`](../../security/config/security_module.go)
    - [`(FirewallOverrideConfiguration).WithRoleHierarchy(roleHierarchy *security.RoleHierarchy) FirewallOverrideConfiguration`](../../security/config/security_module.go)
    - [`(FirewallOverrideConfiguration).WithAccessDecisionManager(accessDecisionManager securitycontract.AccessDecisionManager) FirewallOverrideConfiguration`](../../security/config/security_module.go)
    - [`(FirewallOverrideConfiguration).WithEntryPoint(entryPoint securitycontract.EntryPoint) FirewallOverrideConfiguration`](../../security/config/security_module.go)
    - [`(FirewallOverrideConfiguration).WithAccessDeniedHandler(accessDeniedHandler securitycontract.AccessDeniedHandler) FirewallOverrideConfiguration`](../../security/config/security_module.go)
    - [`(FirewallOverrideConfiguration).WithMergeStrategy(mergeStrategy AccessControlMergeStrategy) FirewallOverrideConfiguration`](../../security/config/security_module.go)
    - [`(FirewallOverrideConfiguration).WithInheritGlobalAccessControl(inheritGlobalAccessControl bool) FirewallOverrideConfiguration`](../../security/config/security_module.go)
- Access control builder:
    - [`NewAccessControlBuilder()`](../../security/config/access_control_builder.go)
    - [`(*AccessControlBuilder).Require(pathPrefix string, attributes ...string)`](../../security/config/access_control_builder.go)
    - [`(*AccessControlBuilder).AllowAnonymous(pathPrefix string)`](../../security/config/access_control_builder.go)
    - [`(*AccessControlBuilder).Build() *security.AccessControl`](../../security/config/access_control_builder.go)
- Access control merge strategies:
    - [`type AccessControlMergeStrategy`](../../security/config/security_module.go)
- Compile:
    - [`Compile(configuration Configuration) (*security.CompiledConfiguration, error)`](../../security/config/compile.go)
