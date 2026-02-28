package handler

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/.example/infra/http/presenter"
    melodyhttp "github.com/precision-soft/melody/http"
    melodyhttpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

func NotFoundHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if true == melodyhttp.PrefersHtml(request) {
            return presenter.HtmlError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
        }

        return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
    }
}
