package handler

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/.example/page"
    melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* CataloguePageHandler serves the read-only catalogue every account holding ROLE_USER opens: the categories and the currencies, read through the doors ROLE_USER already holds, and the links to the editing pages for a role that opens them. */
func CataloguePageHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return page.Html(runtimeInstance, request, nethttp.StatusOK, page.CatalogueHtml), nil
    }
}
