package config

import (
    "errors"

    "github.com/precision-soft/melody/.example/entity"
    "github.com/precision-soft/melody/.example/repository"
    "github.com/precision-soft/melody/.example/route"
    "github.com/precision-soft/melody/.example/security"
    melodyapplication "github.com/precision-soft/melody/application"
    melodycontainer "github.com/precision-soft/melody/container"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecurityconfig "github.com/precision-soft/melody/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

func (instance *Module) RegisterSecurity(builder *melodysecurityconfig.Builder) {
    accessControl := melodysecurity.NewAccessControl(
        /* the index file is the same resource the root serves, so it carries the same policy: MELODY_STATIC_INDEX_FILE makes "/" and "/index.html" two spellings of one page, and a rule anchored at "^/$" would leave the second to the ROLE_USER catch-all below */
        melodysecurity.NewAccessControlRegexRule("^/$", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/index\\.html$", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/login", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/logout", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/routes", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/assets", melodysecuritycontract.AttributePublicAccess),
        melodysecurity.NewAccessControlRegexRule("^/favicon", melodysecuritycontract.AttributePublicAccess),

        /* the monitoring probe answers before there is anyone to authenticate; left to the catch-all below it would answer a monitoring system with a 302 to the login page, or a 401 */
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

    instance.registerApiKeyFirewall(builder)

    override := melodysecurityconfig.NewFirewallOverrideConfiguration()

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

/* registerApiKeyFirewall declares the stateless door APP_API_TOKEN promises: a client presenting X-Api-Key on /products/api is authenticated by the key alone. The matcher claims only requests that present the header, and the firewall is registered before "main", since matching is first-registered-wins and "main" matches every path; a wrong key authenticates as nobody. An empty token leaves the door unwired, which also keeps the authenticator's refusal of an empty expected value from ending the boot. */
func (instance *Module) registerApiKeyFirewall(builder *melodysecurityconfig.Builder) {
    if "" == instance.apiToken {
        return
    }

    builder.AddStatelessFirewall(
        "api",
        security.NewApiKeyRequestMatcher(route.ProductsPrefix+"/api", security.ApiKeyHeaderName),
        []melodysecuritycontract.Rule{},
        melodysecurity.NewAuthenticatorTokenSource(
            melodysecurity.NewAuthenticatorManager(
                melodysecurity.NewApiKeyHeaderAuthenticator(
                    security.ApiKeyHeaderName,
                    instance.apiToken,
                    "api-client",
                    []string{entity.RoleUser, entity.RoleEditor},
                ),
            ),
        ),
        melodysecurityconfig.NewFirewallOverrideConfiguration(),
    )
}

var _ melodyapplication.SecurityModule = (*Module)(nil)

/* sessionUserLookup reads the account through the repository, under the request's context: the user service answers usernames from its cache, and the session's authority must not outlive a change the cache has not dropped yet. */
func sessionUserLookup(request melodyhttpcontract.Request, userId string) (*entity.User, bool, error) {
    runtimeInstance := request.RuntimeInstance()
    if nil == runtimeInstance {
        return nil, false, errors.New("the request carries no runtime to read the account through")
    }

    userRepository, resolveErr := melodycontainer.FromResolver[repository.UserRepository](runtimeInstance.Container(), repository.ServiceUserRepository)
    if nil != resolveErr {
        return nil, false, resolveErr
    }

    return userRepository.FindById(runtimeInstance.Context(), userId)
}
