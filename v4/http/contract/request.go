package contract

import (
    nethttp "net/http"

    bagcontract "github.com/precision-soft/melody/v4/bag/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Request interface {
    HttpRequest() *nethttp.Request

    Parameter(name string) (string, bool)

    Parameters() map[string]string

    Query() bagcontract.ParameterBag

    /* Post answers the urlencoded form of a request whose method is not safe, parsed by its first reader under MaxBodyBytes; the error is the 413 or 400 the failed read answers, the same on every call. A multipart or json body is not read and answers the empty form. */
    Post() (bagcontract.ParameterBag, error)

    /* MaxBodyBytes is the request body cap: the matched route's, else the application's. */
    MaxBodyBytes() int

    Attributes() bagcontract.ParameterBag

    Header(name string) string

    RouteName() string

    RoutePattern() string

    RuntimeInstance() containercontract.Runtime

    RequestContext() RequestContext
}
