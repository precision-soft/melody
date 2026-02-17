package contract

import (
	nethttp "net/http"

	bagcontract "github.com/precision-soft/melody/v2/bag/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type Request interface {
	HttpRequest() *nethttp.Request

	Param(name string) (string, bool)

	Params() map[string]string

	Query() bagcontract.ParameterBag

	Post() bagcontract.ParameterBag

	Attributes() bagcontract.ParameterBag

	Header(name string) string

	RouteName() string

	RoutePattern() string

	RuntimeInstance() runtimecontract.Runtime

	RequestContext() RequestContext
}
