package config

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"
    "time"

    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    bun "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
    melodylockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/redis/rueidis"
)

/* the compose balancer as the shipped .env names it, handed already resolved */
const balancerAddress = "172.18.0.9"

func requestForwardedBy(t *testing.T, peer string, forwardedFor string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    httpRequest.RemoteAddr = peer + ":41234"
    if "" != forwardedFor {
        httpRequest.Header.Set("X-Forwarded-For", forwardedFor)
    }

    return melodyhttp.NewRequest(httpRequest, nil, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("budget-test", time.Now())})
}

func resolvedBudgetKey(t *testing.T, trustedProxyList []string, peer string, forwardedFor string) string {
    t.Helper()

    resolver := requestBudgetConfig(newTrustedProxyResolver(strings.Join(trustedProxyList, ","), time.Now)).ClientIpResolver
    if nil == resolver {
        t.Fatal("expected the request budget to resolve the client address rather than fall back to the peer")
    }

    return resolver(requestForwardedBy(t, peer, forwardedFor))
}

func moduleWithEnvironment(t *testing.T, values map[string]string) *Module {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), &stubEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(environment, "/tmp/melody", melodyconfig.ConfigurationConfig{EnvironmentKeys: EnvironmentKeys()})
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    return &Module{configuration: configuration}
}

type stubEnvironmentSource struct {
    values map[string]string
}

func (instance *stubEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    return instance.values, nil
}

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

type containerRegistrar struct {
    melodycontainercontract.Container
}

func (instance containerRegistrar) RegisterService(
    serviceName string,
    provider any,
    options melodycontainercontract.RegisterOptions,
) {
    instance.MustRegister(serviceName, provider, options)
}

func recoveredRefusal(t *testing.T, step func()) (refusal error) {
    t.Helper()

    defer func() {
        recovered := recover()
        if nil == recovered {
            return
        }

        recoveredError, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the refusal to be an error, got %v", recovered)
        }

        refusal = recoveredError
    }()

    step()

    return nil
}

func configTestRuntime() melodycontainercontract.Runtime {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    return melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
}

func allowedBy(limiter melodyhttpcontract.RateLimiter, key string) bool {
    decision, _ := limiter.Allow(configTestRuntime(), key, melodyhttpcontract.AllowOptions{})

    return decision.Allowed
}

func mustCreateLock(lock melodylockcontract.Lock, createErr error) melodylockcontract.Lock {
    if nil != createErr {
        panic(createErr)
    }

    return lock
}

func redisTestClient(t *testing.T) rueidis.Client {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping the redis backplane test")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{InitAddress: []string{address}})
    if nil != clientErr {
        t.Fatalf("could not connect to redis: %v", clientErr)
    }
    t.Cleanup(client.Close)

    return client
}

func subscribeTestTopic(t *testing.T, hub *melodyhttp.ServerSentEventHub, topic string) *melodyhttp.ServerSentEventSubscriber {
    t.Helper()

    subscriber, subscribeErr := hub.Subscribe(topic, melodyhttp.SubscribeOptions{})
    if nil != subscribeErr {
        t.Fatalf("unexpected subscribe error: %v", subscribeErr)
    }

    return subscriber
}
