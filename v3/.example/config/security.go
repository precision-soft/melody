package config

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/route"
    "github.com/precision-soft/melody/v3/.example/security"
    melodyapplication "github.com/precision-soft/melody/v3/application"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecurityconfig "github.com/precision-soft/melody/v3/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

func (instance *Module) RegisterSecurity(builder *melodysecurityconfig.Builder) {
    accessControl := melodysecurity.NewAccessControl(
        melodysecurity.NewAccessControlRegexRule("^/$", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/login", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/logout", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/routes", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/assets", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/favicon", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/i18n", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/events", melodysecuritycontract.AttributePublicAccess),

        melodysecurity.NewAccessControlRegexRule("^/health", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/metrics", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/openapi.json", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/ws", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/platform/check", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/messagebus/dispatch", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/encrypt/roundtrip", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/redis/token/revocation", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/twofactor", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/outbox", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/storage", melodysecuritycontract.AttributePublicAccess),

        melodysecurity.NewAccessControlRule(route.ProductsPrefix, entity.RoleEditor),
        melodysecurity.NewAccessControlRule(route.CategoriesPrefix, entity.RoleUser),
        melodysecurity.NewAccessControlRule(route.CurrenciesPrefix, entity.RoleUser),
        melodysecurity.NewAccessControlRule(route.UsersPrefix, entity.RoleAdmin),
        melodysecurity.NewAccessControlRule(route.SecurePrefix, entity.RoleUser),
        melodysecurity.NewAccessControlRule(route.InternalPrefix, internalCallerRole),

        melodysecurity.NewAccessControlRegexRule("^/", entity.RoleUser),
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

    /* internal-auth (HMAC) firewall: a stateless machine-to-machine firewall on /internal that verifies
       the signed envelope a caller service sends and authenticates the call as that service principal. */
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

    /* the token firewall's bearer source is decorated with switch-user impersonation: an admin holding
       ROLE_ALLOWED_TO_SWITCH can act as another user by sending X-Switch-User, and the resulting token
       authorizes as the target while keeping the admin readable (and auditable) as the impersonator. */
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
