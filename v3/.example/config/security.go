package config

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/route"
    "github.com/precision-soft/melody/v3/.example/security"
    melodyapplication "github.com/precision-soft/melody/v3/application"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodyaccesscontrol "github.com/precision-soft/melody/v3/security/accesscontrol"
    melodysecurityconfig "github.com/precision-soft/melody/v3/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

func (instance *Module) RegisterSecurity(builder *melodysecurityconfig.Builder) {
    accessControl := melodysecurity.NewAccessControl(
        /* the index file is the same resource the root serves, so it carries the same policy: MELODY_STATIC_INDEX_FILE makes "/" and "/index.html" two spellings of one page, and anchoring the public rule at "^/$" left the explicit spelling to the ROLE_USER catch-all below */
        melodyaccesscontrol.NewRegexRule("^/$", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/index\\.html$", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/login", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/logout", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/routes", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/assets", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/favicon", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/i18n", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),

        melodyaccesscontrol.NewRegexRule("^/health", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/metrics", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/openapi.json", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/platform/check", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/messagebus/dispatch", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/encrypt/roundtrip", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/twofactor", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/outbox", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),
        melodyaccesscontrol.NewRegexRule("^/storage", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),

        /* publishing injects a frame into every stream open across the CLUSTER, so it is the write role the catalog writes themselves carry; streaming carries the catalog writes made behind those roles, so it is at least an authenticated reader, and the handler gates the topic on top of that. Both used to sit under a public "^/events" rule. */
        melodyaccesscontrol.NewSegmentPrefixRule(route.EventsPublishPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.EventsStreamPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),

        /* the websocket door bridges onto the same hub topic the SSE stream gates behind RoleEditor — the catalog topic, carrying every product and user write — and it has no per-topic gate of its own, so the route carries the topic's whole requirement. Public, an anonymous client watched the RoleEditor-gated mutation feed go by. */
        melodyaccesscontrol.NewRegexRule("^/ws", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),

        melodyaccesscontrol.NewSegmentPrefixRule(route.ProductsPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.CategoriesPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.CurrenciesPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.UsersPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleAdmin},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.AccessTokenRevokeUserPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleAdmin},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.AccessTokenPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.DevicePrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.SecurePrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.InternalPrefix, melodyaccesscontrol.RuleConfig{
            Attributes: []string{internalCallerRole},
        }),

        melodyaccesscontrol.NewRegexRule("^/", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),
    )

    roleHierarchy := melodysecurity.NewRoleHierarchy(
        map[string][]string{
            entity.RoleAdmin:  {entity.RoleEditor, entity.RoleUser},
            entity.RoleEditor: {entity.RoleUser},
        },
    )

    accessDecisionManager := melodysecurity.NewAccessDecisionManager(
        melodysecuritycontract.DecisionStrategyAffirmative,
        melodysecurity.NewRoleVoter(),
    )

    entryPoint := security.NewLoginRedirectEntryPoint(route.LoginPagePattern)
    accessDeniedHandler := security.NewDefaultAccessDeniedHandler()

    builder.SetGlobal(
        accessControl,
        roleHierarchy,
        accessDecisionManager,
        entryPoint,
        accessDeniedHandler,
    )

    override := melodysecurityconfig.NewFirewallOverrideConfiguration()

    /* internal-auth (HMAC) firewall: a stateless machine-to-machine firewall on /internal that verifies the signed envelope a caller service sends and authenticates the call as that service principal. */
    builder.AddStatelessFirewall(
        "internal",
        melodysecurity.NewPathPrefixMatcher(route.InternalPrefix),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewHmacTokenSource(melodysecurity.HmacTokenSourceConfig{
            Secrets: instance.hmacSecrets,
            Apps:    instance.hmacApps,
        }),
        melodysecurityconfig.NewFirewallOverrideConfiguration().
            WithEntryPoint(melodysecurity.NewJsonEntryPoint()).
            WithAccessDeniedHandler(melodysecurity.NewJsonAccessDeniedHandler()),
    )

    /* the token firewall's bearer source is decorated with switch-user impersonation: an admin holding ROLE_ALLOWED_TO_SWITCH can act as another user by sending X-Switch-User, and the resulting token authorizes as the target while keeping the admin readable (and auditable) as the impersonator. */
    builder.AddStatelessFirewall(
        "token",
        melodysecurity.NewPathPrefixMatcher(route.SecurePrefix),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewImpersonationTokenSource(melodysecurity.ImpersonationTokenSourceConfig{
            Inner:         melodysecurity.NewBearerTokenSourceWithEnricher(instance.tokenValidator, newScopeRoleEnricher()),
            Users:         instance.impersonatedUsers,
            RoleHierarchy: roleHierarchy,
        }),
        melodysecurityconfig.NewFirewallOverrideConfiguration().
            WithEntryPoint(melodysecurity.NewJsonEntryPoint()).
            WithAccessDeniedHandler(melodysecurity.NewJsonAccessDeniedHandler()),
    )

    builder.AddStatelessFirewall(
        "deviceToken",
        melodysecurity.NewPathPrefixMatcher(route.DevicePrefix),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewBearerTokenSource(instance.opaqueTokenValidator),
        melodysecurityconfig.NewFirewallOverrideConfiguration().
            WithEntryPoint(melodysecurity.NewJsonEntryPoint()).
            WithAccessDeniedHandler(melodysecurity.NewJsonAccessDeniedHandler()),
    )

    builder.AddFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewResolverTokenSource(security.SessionTokenResolver()),
        route.LoginPagePattern,
        route.LogoutPattern,
        security.NewSessionLoginHandler(),
        security.NewSessionLogoutHandler(),
        override,
    )
}

var _ melodyapplication.SecurityModule = (*Module)(nil)
