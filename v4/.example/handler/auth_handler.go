package handler

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/.example/page"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* LoginPageHandler serves the sign-in page; the firewall serves the POST it sends, on its own door (see config/security.go). */
func LoginPageHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return page.Html(runtimeInstance, request, nethttp.StatusOK, page.LoginHtml), nil
    }
}
