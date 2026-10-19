package page

import (
    "embed"
    "encoding/json"
    "errors"
    nethttp "net/http"
    "strings"

    examplesecurity "github.com/precision-soft/melody/.example/security"
    exampleurl "github.com/precision-soft/melody/.example/url"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

const LoginHtml = "login.html"
const CatalogueHtml = "catalogue.html"
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
        routesJson = `{"routes":[]}`
    }

    htmlString = strings.ReplaceAll(htmlString, "{{routes_json}}", escapeForSingleQuotedScript(routesJson))

    /* the viewer's roles, expanded through the hierarchy, decide which links a page offers (data-requires-role); they decide nothing the server allows, since every door a page hides keeps its own access rule */
    viewerJson, viewerJsonErr := json.Marshal(viewerPayload{Roles: examplesecurity.ViewerRoleList(runtimeInstance)})
    if nil != viewerJsonErr {
        viewerJson = []byte(`{"roles":[]}`)
    }

    htmlString = strings.ReplaceAll(htmlString, "{{viewer_json}}", escapeForSingleQuotedScript(string(viewerJson)))

    return melodyhttp.HtmlResponse(statusCode, htmlString)
}

type viewerPayload struct {
    Roles []string `json:"roles"`
}

/* escapeForSingleQuotedScript prepares a json document for a single-quoted JS string literal: a backslash or single quote in it is escaped for that context, backslash first; json.Marshal already escapes < > & and the line separators */
func escapeForSingleQuotedScript(jsonString string) string {
    jsonString = strings.ReplaceAll(jsonString, `\`, `\\`)

    return strings.ReplaceAll(jsonString, `'`, `\'`)
}
