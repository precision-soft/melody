package route

const (
	LoginPageName    = "example.login.page"
	LoginPagePattern = "/login/"

	LoginSubmitName    = "example.login.submit"
	LoginSubmitPattern = "/login/"

	LogoutName    = "example.logout"
	LogoutPattern = "/logout/"

	RoutesName    = "example.routes"
	RoutesPattern = "/routes/"

	/** @info products start */

	ProductsPrefix = "/products"

	ProductsListPageName    = "example.products.list.page"
	ProductsListPagePattern = ProductsPrefix + "/"

	ProductsUpdatePageName    = "example.products.update.page"
	ProductsUpdatePagePattern = ProductsPrefix + "/update/:id/"

	ProductsCreatePageName    = "example.products.create.page"
	ProductsCreatePagePattern = ProductsPrefix + "/create/"

	ProductsApiCreateName    = "example.products.api.create"
	ProductsApiCreatePattern = ProductsPrefix + "/api/create/"

	ProductsApiReadAllName    = "example.products.api.read.all"
	ProductsApiReadAllPattern = ProductsPrefix + "/api/read/"

	ProductsApiReadName    = "example.products.api.read"
	ProductsApiReadPattern = ProductsPrefix + "/api/read/:id/"

	ProductsApiUpdateName    = "example.products.api.update"
	ProductsApiUpdatePattern = ProductsPrefix + "/api/update/:id/"

	ProductsApiDeleteName    = "example.products.api.delete"
	ProductsApiDeletePattern = ProductsPrefix + "/api/delete/:id/"

	/** @info categories start */

	CategoriesPrefix = "/categories"

	CategoriesApiReadAllName    = "example.categories.api.read.all"
	CategoriesApiReadAllPattern = CategoriesPrefix + "/api/read/"

	/** @info currencies start */

	CurrenciesPrefix = "/currencies"

	CurrenciesApiReadAllName    = "example.currencies.api.read.all"
	CurrenciesApiReadAllPattern = CurrenciesPrefix + "/api/read/"

	/** @info users start */

	UsersPrefix = "/users"

	UsersListPageName    = "example.users.list.page"
	UsersListPagePattern = UsersPrefix + "/"

	UsersUpdatePageName    = "example.users.update.page"
	UsersUpdatePagePattern = UsersPrefix + "/update/:id/"

	UsersCreatePageName    = "example.users.create.page"
	UsersCreatePagePattern = UsersPrefix + "/create/"

	UsersApiCreateName    = "example.users.api.create"
	UsersApiCreatePattern = UsersPrefix + "/api/create/"

	UsersApiReadAllName    = "example.users.api.read.all"
	UsersApiReadAllPattern = UsersPrefix + "/api/read/"

	UsersApiReadName    = "example.users.api.read"
	UsersApiReadPattern = UsersPrefix + "/api/read/:id/"

	UsersApiUpdateName    = "example.users.api.update"
	UsersApiUpdatePattern = UsersPrefix + "/api/update/:id/"

	UsersApiDeleteName    = "example.users.api.delete"
	UsersApiDeletePattern = UsersPrefix + "/api/delete/:id/"
)
