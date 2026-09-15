package config

import (
    "github.com/precision-soft/melody/v3/.example/entity"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
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
            Attributes: []string{entity.RoleUser},
        }),

        melodyaccesscontrol.NewRegexRule("^/messagebus/dispatch", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewRegexRule("^/encrypt/roundtrip", melodyaccesscontrol.RuleConfig{
            Attributes: []string{melodysecuritycontract.AttributePublicAccess},
        }),

        melodyaccesscontrol.NewRegexRule("^/twofactor", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),

        melodyaccesscontrol.NewRegexRule("^/outbox", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewRegexRule("^/storage", melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),

        melodyaccesscontrol.NewSegmentPrefixRule(route.EventsPublishPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleEditor},
        }),
        melodyaccesscontrol.NewSegmentPrefixRule(route.EventsStreamPattern, melodyaccesscontrol.RuleConfig{
            Attributes: []string{entity.RoleUser},
        }),

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

        melodyaccesscontrol.NewSegmentPrefixRule(route.ReportsPrefix, melodyaccesscontrol.RuleConfig{
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
        melodysecurity.NewResolverTokenSource(security.SessionTokenResolver(sessionUserLookup)),
        route.LoginPagePattern,
        route.LogoutPattern,
        security.NewSessionLoginHandler(sessionUserLookup),
        security.NewSessionLogoutHandler(),
        override,
    )
}

var _ melodyapplication.SecurityModule = (*Module)(nil)

func sessionUserLookup(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
    runtimeInstance := request.RuntimeInstance()
    userRepository, resolveErr := melodycontainer.FromResolver[repository.UserRepository](runtimeInstance.Container(), repository.ServiceUserRepository)
    if nil != resolveErr {
        return nil, false, resolveErr
    }
    return userRepository.FindById(runtimeInstance.Context(), userId)
}
