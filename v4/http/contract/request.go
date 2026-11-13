package contract

import (
    nethttp "net/http"

    bagcontract "github.com/precision-soft/melody/v4/bag/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type Request interface {
    HttpRequest() *nethttp.Request

    Parameter(name string) (string, bool)

    Parameters() map[string]string

    Query() bagcontract.ParameterBag

    Post() bagcontract.ParameterBag

    Attributes() bagcontract.ParameterBag

    Header(name string) string

    RouteName() string

    RoutePattern() string

    RuntimeInstance() runtimecontract.Runtime

    RequestContext() RequestContext
}
