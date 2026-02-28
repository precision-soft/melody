package bootstrap

import (
    "github.com/precision-soft/melody/.example/domain/entity"
    "github.com/precision-soft/melody/.example/infra/command"
    "github.com/precision-soft/melody/.example/infra/event/subscriber"
    "github.com/precision-soft/melody/.example/infra/http/handler"
    handlercategory "github.com/precision-soft/melody/.example/infra/http/handler/category"
    handlercurrency "github.com/precision-soft/melody/.example/infra/http/handler/currency"
    handlerproduct "github.com/precision-soft/melody/.example/infra/http/handler/product"
    handleruser "github.com/precision-soft/melody/.example/infra/http/handler/user"
    "github.com/precision-soft/melody/.example/infra/http/route"
    "github.com/precision-soft/melody/.example/infra/security"
    melodyapplication "github.com/precision-soft/melody/application"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecurityconfig "github.com/precision-soft/melody/security/config"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

type Module struct{}

func NewExampleModule() *Module {
    return &Module{}
}

func (instance *Module) Name() string {
    return "example"
}

func (instance *Module) Description() string {
    return "melody product catalog example application"
}

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

func (instance *Module) RegisterHttpRoutes(kernelInstance melodykernelcontract.Kernel) {
    router := kernelInstance.HttpRouter()

    kernelInstance.HttpKernel().SetNotFoundHandler(handler.NotFoundHandler())

    router.HandleNamed("example.health", "GET", "/health", handler.HealthHandler())

    router.HandleNamed(route.LoginPageName, "GET", route.LoginPagePattern, handler.LoginPageHandler())
    router.HandleNamed(route.LoginSubmitName, "POST", route.LoginSubmitPattern, handler.LoginHandler())
    router.HandleNamed(route.LogoutName, "GET", route.LogoutPattern, handler.LogoutHandler())

    router.HandleNamed(route.RoutesName, "GET", route.RoutesPattern, handler.RoutesHandler())

    router.HandleNamed(route.CategoriesApiReadAllName, "GET", route.CategoriesApiReadAllPattern, handlercategory.ApiReadAllHandler())

    router.HandleNamed(route.CurrenciesApiReadAllName, "GET", route.CurrenciesApiReadAllPattern, handlercurrency.ApiReadAllHandler())

    router.HandleNamed(route.ProductsListPageName, "GET", route.ProductsListPagePattern, handlerproduct.ListPageHandler())
    router.HandleNamed(route.ProductsCreatePageName, "GET", route.ProductsCreatePagePattern, handlerproduct.CreatePageHandler())
    router.HandleNamed(route.ProductsUpdatePageName, "GET", route.ProductsUpdatePagePattern, handlerproduct.UpdatePageHandler())
    router.HandleNamed(route.ProductsApiCreateName, "POST", route.ProductsApiCreatePattern, handlerproduct.ApiCreateHandler())
    router.HandleNamed(route.ProductsApiReadAllName, "GET", route.ProductsApiReadAllPattern, handlerproduct.ApiReadAllHandler())
    router.HandleNamed(route.ProductsApiReadName, "GET", route.ProductsApiReadPattern, handlerproduct.ApiReadHandler())
    router.HandleNamed(route.ProductsApiUpdateName, "PUT", route.ProductsApiUpdatePattern, handlerproduct.ApiUpdateHandler())
    router.HandleNamed(route.ProductsApiDeleteName, "DELETE", route.ProductsApiDeletePattern, handlerproduct.ApiDeleteHandler())

    router.HandleNamed(route.UsersListPageName, "GET", route.UsersListPagePattern, handleruser.ListPageHandler())
    router.HandleNamed(route.UsersCreatePageName, "GET", route.UsersCreatePagePattern, handleruser.CreatePageHandler())
    router.HandleNamed(route.UsersUpdatePageName, "GET", route.UsersUpdatePagePattern, handleruser.UpdatePageHandler())
    router.HandleNamed(route.UsersApiCreateName, "POST", route.UsersApiCreatePattern, handleruser.ApiCreateHandler())
    router.HandleNamed(route.UsersApiReadAllName, "GET", route.UsersApiReadAllPattern, handleruser.ApiReadAllHandler())
    router.HandleNamed(route.UsersApiReadName, "GET", route.UsersApiReadPattern, handleruser.ApiReadHandler())
    router.HandleNamed(route.UsersApiUpdateName, "PUT", route.UsersApiUpdatePattern, handleruser.ApiUpdateHandler())
    router.HandleNamed(route.UsersApiDeleteName, "DELETE", route.UsersApiDeletePattern, handleruser.ApiDeleteHandler())
}

func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    return []melodyclicontract.Command{
        command.NewAppInfoCommand(),
        command.NewProductListCommand(),
    }
}

func (instance *Module) RegisterEventSubscribers(kernelInstance melodykernelcontract.Kernel) {
    eventDispatcher := kernelInstance.EventDispatcher()

    eventDispatcher.AddSubscriber(
        subscriber.NewProductEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewCategoryEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewCurrencyEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewUserEventSubscriber(),
    )

    eventDispatcher.AddSubscriber(
        subscriber.NewSecurityAuthenticationEventSubscriber(),
    )
}

var _ melodyapplicationcontract.Module = (*Module)(nil)
var _ melodyapplicationcontract.HttpModule = (*Module)(nil)
var _ melodyapplicationcontract.CliModule = (*Module)(nil)
var _ melodyapplicationcontract.EventModule = (*Module)(nil)
var _ melodyapplication.SecurityModule = (*Module)(nil)
