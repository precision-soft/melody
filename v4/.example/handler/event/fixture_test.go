package event

import (
    "testing"

    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
)

/* routedRequestAs is a request the router matched to the route named, for a caller the firewall authenticated with the roles given */
func routedRequestAs(t *testing.T, routeName string, target string, roles []string) (*melodyhttp.Request, melodycontainercontract.Runtime) {
    t.Helper()

    request, runtimeInstance := streamRequestAs(t, target, roles)
    request.Attributes().Set(melodyhttp.RouteAttributeName, routeName)

    return request, runtimeInstance
}
