package user

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/.example/infra/http/page"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type adminUserCreateRequest struct {
    Username string   `json:"username"`
    Password string   `json:"password"`
    Roles    []string `json:"roles"`
}

type userCurrentResponse struct {
    UserId string   `json:"userId"`
    Roles  []string `json:"roles"`
}

func ListPageHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return page.Html(runtimeInstance, request, nethttp.StatusOK, page.UsersHtml), nil
    }
}

func UpdatePageHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return page.Html(runtimeInstance, request, nethttp.StatusOK, page.ProfileHtml), nil
    }
}

func CreatePageHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return page.Html(runtimeInstance, request, nethttp.StatusOK, page.ProfileHtml), nil
    }
}
