package config

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

/* the compose balancer as the shipped .env names it, already resolved: the tests below hand the list in resolved form because what they assert is what the resolver does with a peer and a chain, not what the name resolves to. */
const balancerAddress = "172.18.0.9"

/* a request as a proxy delivers it: the peer is the given address and the client it forwarded is named in the header. */
func requestForwardedBy(t *testing.T, peer string, forwardedFor string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    httpRequest.RemoteAddr = peer + ":41234"
    if "" != forwardedFor {
        httpRequest.Header.Set("X-Forwarded-For", forwardedFor)
    }

    return melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("budget-test", time.Now()))
}

/* resolvedBudgetKey reads the key the budget charges, over a resolver whose entries are the given list — addresses and prefixes taken as written, so no name is looked up here; what these tests assert is what the resolver does with a peer and a header, not how a name resolves */
func resolvedBudgetKey(t *testing.T, trustedProxyList []string, peer string, forwardedFor string) string {
    t.Helper()

    resolver := requestBudgetConfig(100, newTrustedProxyResolver(strings.Join(trustedProxyList, ","), time.Now)).ClientIpResolver()
    if nil == resolver {
        t.Fatal("expected the request budget to resolve the client address rather than fall back to the peer")
    }

    return resolver(requestForwardedBy(t, peer, forwardedFor))
}

func moduleWithEnvironment(t *testing.T, values map[string]string) *Module {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(&stubEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    return &Module{configuration: configuration}
}

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load() (map[string]string, error) {
    return instance.values, nil
}

/* refusingConnector stands in for a driver that is never dialed. database/sql opens lazily, and nothing in
   these tests issues a query, so a connector that refuses every connection is the cheapest handle that is a
   real *bun.DB: it proves the tests measure the wiring rather than a server. */
type refusingConnector struct{}

func (instance *refusingConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return nil, errors.New("this handle is never dialed")
}

func (instance *refusingConnector) Driver() driver.Driver {
    return nil
}

func newUndialedDatabase() *bun.DB {
    return bun.NewDB(sql.OpenDB(&refusingConnector{}), mysqldialect.New())
}

/* containerRegistrar is the container under the name-based registrar the composition root is handed. The
   application supplies this method by delegating to MustRegister; the container itself carries only the
   Registrar half, so a test that drives a registration door needs the same one-line adapter. */
type containerRegistrar struct {
    melodycontainercontract.Container
}

func (instance containerRegistrar) RegisterService(
    serviceName string,
    provider any,
    options ...melodycontainercontract.RegisterOption,
) {
    instance.MustRegister(serviceName, provider, options...)
}
