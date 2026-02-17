package handler

import (
	nethttp "net/http"

	"github.com/precision-soft/melody/v2/.example/infra/http/presenter"
	melodyhttp "github.com/precision-soft/melody/v2/http"
	melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
	melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func NotFoundHandler() melodyhttpcontract.Handler {
	return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
		if true == melodyhttp.PrefersHtml(request) {
			return presenter.HtmlError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
		}

		return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "not found"), nil
	}
}
