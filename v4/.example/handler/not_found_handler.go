package handler

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/.example/presenter"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
)

func NotFoundHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if true == melodyhttp.PrefersHtml(request) {
            return presenter.HtmlError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
        }

        return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
    }
}
