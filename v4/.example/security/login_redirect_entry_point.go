package security

import (
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

func NewLoginRedirectEntryPoint(loginPath string) melodysecuritycontract.EntryPoint {
    return &loginRedirectEntryPoint{
        loginPath: loginPath,
    }
}

type loginRedirectEntryPoint struct {
    loginPath string
}

func (instance *loginRedirectEntryPoint) Start(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
    if true == melodyhttp.PrefersHtml(request) {
        return melodyhttp.RedirectFound(instance.loginPath), nil
    }

    return melodyhttp.JsonErrorResponse(
        401,
        "unauthorized",
    ), nil
}

var _ melodysecuritycontract.EntryPoint = (*loginRedirectEntryPoint)(nil)
