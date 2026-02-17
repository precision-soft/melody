package page

import (
	"embed"
	"errors"
	nethttp "net/http"
	"strings"

	exampleurl "github.com/precision-soft/melody/v2/.example/infra/http/url"
	melodyhttp "github.com/precision-soft/melody/v2/http"
	melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
	melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

const LoginHtml = "login.html"
const ProductsListHtml = "product_list.html"
const ProductsDetailsHtml = "product_detail.html"
const ProfileHtml = "profile.html"
const UsersHtml = "users.html"

var ErrPageNotFound = errors.New("page not found")

//go:embed *.html
var pages embed.FS

func Load(fileName string) (string, error) {
	content, err := pages.ReadFile(fileName)
	if nil != err {
		return "", ErrPageNotFound
	}

	return string(content), nil
}

func Html(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, statusCode int, fileName string) melodyhttpcontract.Response {
	_ = request

	htmlString, err := Load(fileName)
	if nil != err {
		return melodyhttp.JsonErrorResponse(nethttp.StatusInternalServerError, "failed to load page")
	}

	routesJson, routesJsonErr := exampleurl.RoutesJsonFromContainer(runtimeInstance.Container())
	if nil != routesJsonErr {
		routesJson = "[]"
	}

	htmlString = strings.ReplaceAll(htmlString, "{{routes_json}}", routesJson)

	return melodyhttp.HtmlResponse(statusCode, htmlString)
}
