package config

import (
    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/route"
    "github.com/precision-soft/melody/.example/security"
    melodyapplication "github.com/precision-soft/melody/application"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecurityconfig "github.com/precision-soft/melody/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

func (instance *Module) RegisterSecurity(builder *melodysecurityconfig.Builder) {
    accessControl := melodysecurity.NewAccessControl(
        /* the index file is the same resource the root serves, so it carries the same policy: MELODY_STATIC_INDEX_FILE makes "/" and "/index.html" two spellings of one page, and anchoring the public rule at "^/$" left the explicit spelling to the ROLE_USER catch-all below */
        melodysecurity.NewAccessControlRegexRule("^/$", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/index\\.html$", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/login", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/logout", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/routes", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/assets", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/favicon", melodysecuritycontract.AttributePublicAccess),

        /* the monitoring probe answers before there is anyone to authenticate: registered as a route by config/http.go and left to the catch-all below, it answered a monitoring system with a 302 to the login page, or a 401 to one that does not ask for html */
        melodysecurity.NewAccessControlRegexRule("^/health", melodysecuritycontract.AttributePublicAccess),

        melodysecurity.NewAccessControlRule(route.ProductsPrefix, entity.RoleEditor),
        melodysecurity.NewAccessControlRule(route.CategoriesPrefix, entity.RoleUser),
        melodysecurity.NewAccessControlRule(route.CurrenciesPrefix, entity.RoleUser),
        melodysecurity.NewAccessControlRule(route.UsersPrefix, entity.RoleAdmin),

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
