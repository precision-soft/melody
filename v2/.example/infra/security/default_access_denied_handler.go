package security

import (
	melodyhttp "github.com/precision-soft/melody/v2/http"
	melodyhttpcontract "github.com/precision-soft/melody/v2/http/contract"
	melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
	melodysecuritycontract "github.com/precision-soft/melody/v2/security/contract"
)

func NewDefaultAccessDeniedHandler() melodysecuritycontract.AccessDeniedHandler {
	return &defaultAccessDeniedHandler{}
}

type defaultAccessDeniedHandler struct {
}

func (instance *defaultAccessDeniedHandler) Handle(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, decisionErr error) (melodyhttpcontract.Response, error) {
	if true == isHtmlRequest(request) {
		return melodyhttp.HtmlResponse(
			403,
			"<h1>Forbidden</h1>",
		), nil
	}

	return melodyhttp.JsonErrorResponse(
		403,
		"forbidden",
	), nil
}
