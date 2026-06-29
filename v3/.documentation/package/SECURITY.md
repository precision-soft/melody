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
package main

import (
	"errors"

	applicationcontract "github.com/precision-soft/melody/v3/application/contract"
	httpcontract "github.com/precision-soft/melody/v3/http/contract"
	kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
	"github.com/precision-soft/melody/v3/security"
	securityconfig "github.com/precision-soft/melody/v3/security/config"
	securitycontract "github.com/precision-soft/melody/v3/security/contract"
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

### Token authentication (stateless Bearer)

For stateless APIs, [`BearerTokenSource`](../../security/bearer_token_source.go) extracts an `Authorization: Bearer <token>` header and delegates validation to a pluggable [`TokenValidator`](../../security/contract/token_validator.go). Two validators ship in the package:

- [`JwtTokenValidator`](../../security/jwt_token_validator.go) — verifies HS256 JWTs with a shared secret (stdlib only, no external dependency), checks `exp`/`nbf`, and maps the subject and roles claims to [`Claims`](../../security/contract/token_validator.go). The `exp` (expiry) claim is **required by default** — a token without `exp` is rejected unless `JwtConfig{AllowWithoutExpiry: true}` is set, so a missing expiry never silently yields a non-expiring token. Out-of-range or non-finite `exp`/`nbf`/`iat` `NumericDate` values are rejected as malformed rather than saturating on the int64 conversion. A token with an empty, absent, or non-string subject is rejected (it must never authenticate as the empty principal `""`). A future `iat` is accepted by default (RFC 7519 treats `iat` as informational); set `JwtConfig.RejectFutureIssuedAt` to reject it instead. Self-contained; no per-request lookup.
- [`OpaqueTokenValidator`](../../security/opaque_token_validator.go) — looks the token up in a [`TokenStore`](../../security/contract/token_store.go), so tokens are revocable (a stored token with an empty subject is rejected). [`InMemoryTokenStore`](../../security/in_memory_token_store.go) ships for tests/dev; the `integrations/rueidis` `NewTokenStore` is a production Redis-backed [`RevocableTokenStore`](../../security/contract/token_store.go) (the `TokenStore` lookup interface plus `Put`/`PutWithTtl`/`Delete`/`DeleteByUser`/`PurgeExpired`), keeping the full revocation surface behind the interface so the firewall wiring is identical. Behind a load balancer use the Redis store: the in-memory store is per-process, so a token issued or revoked on one instance is invisible to the others (revocation would not take effect cluster-wide). Run `PurgeExpired` on a schedule from a single instance (e.g. a cron command) rather than from every instance — Redis expires the token keys natively; the
  purge only reconciles the user index. Roles enrichment runs only via the bearer source's enricher, not the validator.

A failed or missing token resolves to an anonymous token, so the firewall's entry point decides the response. [`JsonEntryPoint`](../../security/json_entry_point.go) (401) and [`JsonAccessDeniedHandler`](../../security/json_access_denied_handler.go) (403) return JSON instead of redirecting — set them globally for pure-API apps.

```go
func (instance *apiSecurityModule) RegisterSecurity(builder *securityconfig.Builder) {
	validator := security.NewJwtTokenValidator(security.JwtConfig{
		Secret: []byte(jwtSecret),
	})

	builder.SetGlobal(
		accessControl,
		roleHierarchy,
		accessDecisionManager,
		security.NewJsonEntryPoint(),
		security.NewJsonAccessDeniedHandler(),
	)

	builder.AddStatelessFirewall(
		"api",
		security.NewPathPrefixMatcher("/api"),
		[]securitycontract.Rule{},
		security.NewBearerTokenSource(validator),
		securityconfig.NewFirewallOverrideConfiguration(),
	)
}
```

The example application wires a stateless `/secure` firewall (`config/security.go`), a protected handler (`handler/secure/me_handler.go`), and a demo JWT minting command (`cli/auth_token_command.go`, `auth:token`). A stateless firewall must be registered before a broader catch-all firewall, since the first matching firewall wins.

#### Resolving roles after validation (enrichment hook)

When the token only carries an opaque scope (e.g. a tenant/application identifier) and the real roles live in a database, implement the generic [`TokenEnricher`](../../security/contract/token_enricher.go) and wire it with [`NewBearerTokenSourceWithEnricher`](../../security/bearer_token_source.go). It runs **after** the signature is validated and turns the token's `Claims.Scope` into the final roles/attributes. The library ships only the interface and the wiring — any tenant- or product-specific resolution lives in your enricher, keeping the security package generic. An enrichment error falls back to an anonymous token (the firewall then decides the response); the error is logged at INFO level so operators can observe the failure and is not propagated to the handler.

The enriched [`AuthenticatedToken`](../../security/authenticated_token.go) carries the resolved `Scope()` and `Attributes()` alongside `Roles()` (both accessors return defensive copies), so attribute-based access control downstream can read the tenant/attribute data the enricher attached — not only the roles.

To feed the enricher, the JWT validator can copy an object claim into `Claims.Scope` via `JwtConfig.ScopeClaim`:

```go
validator := security.NewJwtTokenValidator(security.JwtConfig{
	Secret:     []byte(jwtSecret),
	ScopeClaim: "scope", // copies the `scope` object claim into Claims.Scope
})

// scopeRoleEnricher resolves roles from the token scope (here, a static map;
// in a real app this would be a database lookup keyed by Claims.Scope).
source := security.NewBearerTokenSourceWithEnricher(validator, scopeRoleEnricher{})
```

`Claims` exposes generic `Scope` and `Attributes` maps for this purpose; the library assigns no meaning to their keys.

### Internal service-to-service authentication (HMAC)

For machine-to-machine calls between trusted services, [`HmacTokenSource`](../../security/hmac_token_source.go) verifies an HMAC-signed envelope carried on the `X-Melody-Internal-Auth` header and resolves it to the *calling service* as the principal. The matching client helper is [`HmacEnvelopeSigner`](../../security/hmac_signer.go). The envelope binds the call to its method, path and query string, an issued/expiry window, a single-use nonce, and a hash of the request body, so a captured envelope cannot be replayed against another route, after expiry, twice, or with a tampered body or query. Replay is rejected by a pluggable [`NonceGuard`](../../security/contract/nonce_guard.go) (defaults to an in-process [`MemoryNonceGuard`](../../security/memory_nonce_guard.go); supply a shared guard such as the rueidis Redis nonce guard for multi-instance deployments).

The signed payload optionally carries an **originating actor** (F1) via [`Actor`](../../security/actor.go) / [`Token.OnBehalfOf()`](../../security/contract/token.go), so service B authorizes and audits the call as the upstream user/client that started it, without that user re-authenticating to B. Read it back with [`ActorFromToken`](../../security/actor.go).

**Key-id ↔ app binding (trust model).** Each key id is issued to **exactly one application**. The secret provider ([`NewStaticHmacSecretProvider`](../../security/hmac_secret_provider.go), entries are `HmacKey{App, Secret}`) records that binding, the [`HmacAppRegistry`](../../security/hmac_app_registry.go) maps an app to the roles its verified principal receives, and the verifier **refuses an envelope whose key id is not bound to the app it claims**. This is what stops a holder of one valid secret from claiming a higher-privileged app (and forging an arbitrary actor): a secret is only ever as privileged as the single app its key id is issued to. Because the key id is attacker-visible, the binding only isolates apps when their secret material is distinct, so `NewStaticHmacSecretProvider` rejects the same secret bytes registered under key ids belonging to different apps. The signer fails fast at construction if its current key id is not bound to the app it signs for. Rotate by issuing a second key id bound to the same app, rolling `CurrentKeyId` to it, then retiring the old key id once every caller has moved.

The embedded actor stays self-asserted: an *authenticated* app is trusted to state who it acts on behalf of, exactly as it is trusted with its own secret. Binding the app identity bounds actor forgery to what each app is already trusted to assert — keep one key id per app and rotate the shared secrets like any other credential.

## Footguns & caveats

- `AccessControl` uses a deterministic match priority: exact match first, then longest prefix match (including segment-prefix rules), then regex rules in the order they were registered, then the empty-prefix fallback. See [`(*AccessControl).Match`](../../security/access_control.go).
- Internal-auth HMAC key ids are bound to a single app: do not share one key id (or its secret) across applications — the verifier rejects any envelope whose key id is not bound to its claimed app, and a shared secret would otherwise let one service impersonate another. Supply a shared [`NonceGuard`](../../security/contract/nonce_guard.go) behind a load balancer; the default in-process guard only prevents replay within one instance.
- `SecurityContextSetOnRuntime` stores the context in the runtime scope under `security/contract.ServiceSecurityContext`.
- `JwtTokenValidator` requires the `exp` claim by default: a signed token without `exp` is rejected unless you set `JwtConfig{AllowWithoutExpiry: true}`. This differs from RFC 7519, which treats registered claims as optional — so a token that looks valid but omits `exp` resolves to an anonymous token, not an authenticated one.

## Userland API

### Contracts (`security/contract`)

#### Types

- [`Rule`](../../security/contract/rule.go)
- [`Matcher`](../../security/contract/matcher.go)
- [`Token`](../../security/contract/token.go)
- [`TokenSource`](../../security/contract/token_source.go)
- [`Authenticator`](../../security/contract/authenticator.go)
- [`TokenValidator`](../../security/contract/token_validator.go)
- [`TokenEnricher`](../../security/contract/token_enricher.go)
- [`Claims`](../../security/contract/token_validator.go)
- [`TokenStore`](../../security/contract/token_store.go)
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
- Token auth: [`BearerTokenSource`](../../security/bearer_token_source.go), [`JwtTokenValidator`](../../security/jwt_token_validator.go), [`JwtConfig`](../../security/jwt_token_validator.go), [`OpaqueTokenValidator`](../../security/opaque_token_validator.go), [`InMemoryTokenStore`](../../security/in_memory_token_store.go), [`JsonEntryPoint`](../../security/json_entry_point.go), [`JsonAccessDeniedHandler`](../../security/json_access_denied_handler.go)
- Matchers: [`PathPrefixMatcher`](../../security/matcher.go)
- Authorization: [`AccessDecisionManager`](../../security/access_decision_manager.go), [`RoleVoter`](../../security/voter.go), [`RoleHierarchyVoter`](../../security/role_hierarchy_voter.go)
- Token source: [`ResolverTokenSource`](../../security/token_source.go)
- Events: [`AuthorizationGrantedEvent`](../../security/authorization_granted_event.go), [`AuthorizationDeniedEvent`](../../security/authorization_denied_event.go), [`LoginSuccessEvent`](../../security/login_success_event.go), [`LoginFailureEvent`](../../security/login_failure_event.go), [`LogoutSuccessEvent`](../../security/logout_success_event.go), [`LogoutFailureEvent`](../../security/logout_failure_event.go)
- Configuration: [`CompiledConfiguration`, `CompiledFirewall`](../../security/compiled_configuration.go)
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
- [`NewBearerTokenSource(validator securitycontract.TokenValidator)`](../../security/bearer_token_source.go)
- [`NewBearerTokenSourceWithEnricher(validator securitycontract.TokenValidator, enricher securitycontract.TokenEnricher)`](../../security/bearer_token_source.go)
- [`NewJwtTokenValidator(config JwtConfig)`](../../security/jwt_token_validator.go)
- [`NewOpaqueTokenValidator(store securitycontract.TokenStore)`](../../security/opaque_token_validator.go)
- [`NewInMemoryTokenStore()`](../../security/in_memory_token_store.go)
- [`NewInMemoryTokenStoreWithClock(clockInstance clockcontract.Clock)`](../../security/in_memory_token_store.go)
- [`NewResolverTokenSource(resolver securitycontract.TokenResolver)`](../../security/token_source.go)
- [`NewJsonEntryPoint()`](../../security/json_entry_point.go)
- [`NewJsonAccessDeniedHandler()`](../../security/json_access_denied_handler.go)
- [`NewAccessDecisionManager(strategy securitycontract.DecisionStrategy, voters ...securitycontract.Voter)`](../../security/access_decision_manager.go)
- [`NewAccessDecisionManagerWithVoters(strategy securitycontract.DecisionStrategy, voters []securitycontract.Voter)`](../../security/access_decision_manager.go)
- [`NewRoleVoter()`](../../security/voter.go)
- [`NewRoleHierarchyVoter(roleHierarchy *RoleHierarchy, delegate *RoleVoter)`](../../security/role_hierarchy_voter.go)
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
