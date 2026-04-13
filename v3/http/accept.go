package http

import (
    "strings"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func PrefersHtml(request httpcontract.Request) bool {
    if nil == request {
        return false
    }

    httpRequest := request.HttpRequest()
    if nil == httpRequest {
        return false
    }

    acceptHeader := httpRequest.Header.Get("Accept")
    if "" == acceptHeader {
        return false
    }

    acceptHeaderLower := strings.ToLower(acceptHeader)

    htmlIndex := strings.Index(acceptHeaderLower, "text/html")
    jsonIndex := strings.Index(acceptHeaderLower, "application/json")

    if 0 <= htmlIndex {
        if 0 > jsonIndex {
            return true
        }

        return htmlIndex < jsonIndex
    }

    return false
}
