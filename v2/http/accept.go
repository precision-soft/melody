package http

import (
	"strings"

	httpcontract "github.com/precision-soft/melody/v2/http/contract"
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

	if true == strings.Contains(acceptHeaderLower, "text/html") && false == strings.Contains(acceptHeaderLower, "application/json") {
		return true
	}

	return false
}
