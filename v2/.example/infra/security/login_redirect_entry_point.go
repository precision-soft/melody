package security

import (
    "strings"

    melodyhttp "github.com/precision-soft/melody/v2/http"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewLoginRedirectEntryPoint(loginPath string) melodysecuritycontract.EntryPoint {
    return &loginRedirectEntryPoint{
        loginPath: loginPath,
    }
}

type loginRedirectEntryPoint struct {
    loginPath string
}

func (instance *loginRedirectEntryPoint) Start(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
    if true == isHtmlRequest(request) {
        return melodyhttp.RedirectFound(instance.loginPath), nil
    }

    return melodyhttp.JsonErrorResponse(
        401,
        "unauthorized",
    ), nil
}

func isHtmlRequest(request melodyhttpcontract.Request) bool {
    if nil == request || nil == request.HttpRequest() {
        return false
    }

    acceptHeader := request.HttpRequest().Header.Get("Accept")
    if "" == acceptHeader {
        return false
    }

    if true == strings.Contains(acceptHeader, "text/html") {
        return true
    }

    return false
}
