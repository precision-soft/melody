package contract

type UrlGenerator interface {
	GeneratePath(routeName string, params map[string]string) (string, error)

	GenerateUrl(routeName string, params map[string]string, queryParams map[string]string) (string, error)
}
