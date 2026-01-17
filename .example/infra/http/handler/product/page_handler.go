package product

import (
	nethttp "net/http"

	"github.com/precision-soft/melody/.example/infra/http/page"
	melodyhttpcontract "github.com/precision-soft/melody/http/contract"
	melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

func ListPageHandler() melodyhttpcontract.Handler {
	return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
		return page.Html(runtimeInstance, request, nethttp.StatusOK, page.ProductsListHtml), nil
	}
}

func UpdatePageHandler() melodyhttpcontract.Handler {
	return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
		return page.Html(runtimeInstance, request, nethttp.StatusOK, page.ProductsDetailsHtml), nil
	}
}

func CreatePageHandler() melodyhttpcontract.Handler {
	return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
		return page.Html(runtimeInstance, request, nethttp.StatusOK, page.ProductsDetailsHtml), nil
	}
}
