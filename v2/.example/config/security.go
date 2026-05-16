package config

import (
    "github.com/precision-soft/melody/v2/.example/entity"
    "github.com/precision-soft/melody/v2/.example/route"
    "github.com/precision-soft/melody/v2/.example/security"
    melodyapplication "github.com/precision-soft/melody/v2/application"
    melodysecurity "github.com/precision-soft/melody/v2/security"
    melodysecurityconfig "github.com/precision-soft/melody/v2/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
)

func (instance *Module) RegisterSecurity(builder *melodysecurityconfig.Builder) {
    accessControl := melodysecurity.NewAccessControl(
        melodysecurity.NewAccessControlRegexRule("^/$", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/login", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/logout", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/routes", melodysecuritycontract.AttributePublicAccess),

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
