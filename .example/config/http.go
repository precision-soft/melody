package config

import (
    "github.com/precision-soft/melody/.example/handler"
    handlercategory "github.com/precision-soft/melody/.example/handler/category"
    handlercurrency "github.com/precision-soft/melody/.example/handler/currency"
    handlerproduct "github.com/precision-soft/melody/.example/handler/product"
    handleruser "github.com/precision-soft/melody/.example/handler/user"
    "github.com/precision-soft/melody/.example/route"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
)

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

var _ melodyapplicationcontract.HttpModule = (*Module)(nil)
