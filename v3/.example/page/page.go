package page

import (
    "embed"
    "errors"
    nethttp "net/http"
    "strings"

    exampleurl "github.com/precision-soft/melody/v3/.example/url"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    examplejournal "github.com/precision-soft/melody/v3/.example/journal"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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
    htmlString, err := Load(fileName)
    if nil != err {
        return melodyhttp.JsonErrorResponse(nethttp.StatusInternalServerError, "failed to load page")
    }

    /* a page without its manifest still renders, since a page that fails to load is worse than one whose links fail, but the loss is journaled rather than carried to the browser in silence */
    routesJson, routesJsonErr := exampleurl.RoutesJsonFromRuntime(runtimeInstance)
    if nil != routesJsonErr {
        routesJson = exampleurl.EmptyRoutesJson
        examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Warning(
            "page rendered without its route manifest",
            melodyexception.LogContext(routesJsonErr, melodyloggingcontract.Context{"page": fileName}),
        )
    }

    /* the manifest is embedded inside a single-quoted JS string literal (window.melodyRoutes = JSON.parse('...')), so a backslash or single quote in the JSON must be escaped for that context or a crafted route name/pattern would break out of the string; json.Marshal already escapes < > & and the line separators, so escaping \ and ' is sufficient (backslash first so the quote escape is not re-escaped). */
    routesJson = strings.ReplaceAll(routesJson, `\`, `\\`)
    routesJson = strings.ReplaceAll(routesJson, `'`, `\'`)

    htmlString = strings.ReplaceAll(htmlString, "{{routes_json}}", routesJson)

    return melodyhttp.HtmlResponse(statusCode, htmlString)
}
