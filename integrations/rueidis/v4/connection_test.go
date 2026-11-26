package rueidis

import (
    "context"
    "errors"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/redis/rueidis"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
)

func TestNewConnection_ANilClientOrAMalformedNamespaceIsRefused(t *testing.T) {
    var typedNilClient *blockingCloseClient

    for _, testCase := range []struct {
        name      string
        client    rueidis.Client
        namespace string
        argument  string
    }{
        {name: "nil client", client: nil, namespace: "melody", argument: "client"},
        {name: "typed nil client", client: typedNilClient, namespace: "melody", argument: "client"},
        {name: "empty namespace", client: &stubClient{}, namespace: "", argument: "namespace"},
        {name: "namespace with a separator", client: &stubClient{}, namespace: "a:b", argument: "namespace"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            expectPanicNamingTheArgument(t, testCase.argument, func() {
                NewConnection(testCase.client, testCase.namespace)
            })
        })
    }

    if connection := NewConnection(&stubClient{}, "shop"); "shop" != connection.Namespace() {
        t.Fatalf("expected the namespace served as handed, got %q", connection.Namespace())
    }
}

func TestConnection_ClosesTheClientOnceAndStaysIdempotent(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS is not set")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{
        InitAddress:  []string{address},
        DisableCache: true,
    })
    if nil != clientErr {
        t.Fatalf("client: %v", clientErr)
    }

    connection := NewConnection(client, "melody")

    if client != connection.Client() {
        t.Fatal("expected the wrapped client to be served")
    }

    if closeErr := connection.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("first close: %v", closeErr)
    }

    if closeErr := connection.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("second close must stay nil, got %v", closeErr)
    }

    pingErr := client.Do(context.Background(), client.B().Ping().Build()).Error()
    if nil == pingErr {
        t.Fatal("expected the wrapped client to be closed")
    }
}

type blockingCloseClient struct {
    rueidis.Client
    closeEntered chan struct{}
    releaseClose chan struct{}
}

func (instance *blockingCloseClient) Close() {
    close(instance.closeEntered)
    <-instance.releaseClose
}

func TestConnection_AClientCloseOutlivingTheDeadlineAnswersTheDeadlineAndFinishesInTheBackground(t *testing.T) {
    client := &blockingCloseClient{closeEntered: make(chan struct{}), releaseClose: make(chan struct{})}
    connection := NewConnection(client, "melody")

    closeContext, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
    defer cancel()

    closeErr := connection.CloseWithContext(closeContext)
    if false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline's error over a close still running, got %v", closeErr)
    }

    <-client.closeEntered
    close(client.releaseClose)

    if laterErr := connection.CloseWithContext(t.Context()); nil != laterErr {
        t.Fatalf("expected the finished close to answer nil, got %v", laterErr)
    }
}

type panickingCloseClient struct {
    rueidis.Client
}

func (instance *panickingCloseClient) Close() {
    panic(errors.New("the driver tore its own state down"))
}

func TestConnection_APanickingClientCloseIsAnsweredAsTheClosesErrorEveryTime(t *testing.T) {
    connection := NewConnection(&panickingCloseClient{}, "melody")

    for attempt := 0; attempt < 2; attempt++ {
        closeErr := connection.CloseWithContext(t.Context())
        if nil == closeErr || false == strings.Contains(closeErr.Error(), "panicked") {
            t.Fatalf("attempt %d: expected the panic answered as the close's error, got %v", attempt, closeErr)
        }
    }
}

func TestConnection_EveryComponentDerivesItsDefaultNameFromTheNamespace(t *testing.T) {
    connection := NewConnection(&stubClient{}, "shop")

    for name, actual := range map[string]string{
        "rate limiter": NewRateLimiter(connection, "login", 1, time.Second, RateLimiterConfig{}).prefix,
        "nonce guard":  NewNonceGuard(connection, NonceGuardConfig{}).keyPrefix,
        "token store":  NewTokenStore(connection, TokenStoreConfig{}).prefix,
        "locker":       NewLocker(connection, LockerConfig{}).keyPrefix,
    } {
        expected := map[string]string{"rate limiter": "shop:rate_limit:", "nonce guard": "shop:nonce", "token store": "shop:token", "locker": "shop:lock:"}[name]
        if expected != actual {
            t.Fatalf("%s: expected the default %q, got %q", name, expected, actual)
        }
    }

    backplane := NewServerSentEventBackplane(NewConnection(newTokenStoreClient(t), "shop"), melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), ServerSentEventBackplaneConfig{})
    defer backplane.CloseWithContext(context.Background())

    if "shop:sse" != backplane.channel {
        t.Fatalf("expected the backplane channel shop:sse, got %q", backplane.channel)
    }
}

func TestConnection_TwoNamespacesOnOneClientShareNothing(t *testing.T) {
    client := newTokenStoreClient(t)
    runtimeInstance := newLockRuntime()

    shop := NewConnection(client, "shop")
    store := NewConnection(client, "store")

    name := "twin-" + strings.ReplaceAll(t.Name(), "/", "-")
    shopLock := mustCreateLock(NewLocker(shop, LockerConfig{}).CreateLock(name, bound.Of(10*time.Second)))
    storeLock := mustCreateLock(NewLocker(store, LockerConfig{}).CreateLock(name, bound.Of(10*time.Second)))
    defer func() {
        _ = shopLock.Release(runtimeInstance)
        _ = storeLock.Release(runtimeInstance)
    }()

    for label, lock := range map[string]interface {
        Acquire(runtimeInstance containercontract.Runtime) (bool, error)
    }{"shop": shopLock, "store": storeLock} {
        if acquired, acquireErr := lock.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
            t.Fatalf("%s: expected the lock of one name to be its namespace's own, got %v %v", label, acquired, acquireErr)
        }
    }

    shopLimiter := NewRateLimiter(shop, name, 1, time.Minute, RateLimiterConfig{})
    storeLimiter := NewRateLimiter(store, name, 1, time.Minute, RateLimiterConfig{})
    defer func() {
        _ = shopLimiter.Reset(runtimeInstance, "client")
        _ = storeLimiter.Reset(runtimeInstance, "client")
    }()

    for label, limiter := range map[string]*RateLimiter{"shop": shopLimiter, "store": storeLimiter} {
        if decision, allowErr := limiter.Allow(runtimeInstance, "client"); nil != allowErr || false == decision.Allowed {
            t.Fatalf("%s: expected the first request of its own counter admitted, got %+v %v", label, decision, allowErr)
        }
    }

    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    shopBackplane := NewServerSentEventBackplane(shop, hub, ServerSentEventBackplaneConfig{})
    defer shopBackplane.CloseWithContext(context.Background())

    storeBackplane := NewServerSentEventBackplane(store, melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), ServerSentEventBackplaneConfig{})
    defer storeBackplane.CloseWithContext(context.Background())

    if shopBackplane.channel == storeBackplane.channel {
        t.Fatalf("expected the backplanes of two namespaces on distinct channels, both on %q", shopBackplane.channel)
    }
}

func TestComponent_AnExplicitNameIsUsedVerbatim(t *testing.T) {
    connection := NewConnection(&stubClient{}, "shop")

    for name, pair := range map[string][2]string{
        "rate limiter": {"legacy:rate_limit:", NewRateLimiter(connection, "login", 1, time.Second, RateLimiterConfig{KeyPrefix: "legacy:rate_limit:"}).prefix},
        "nonce guard":  {"legacy:nonce", NewNonceGuard(connection, NonceGuardConfig{KeyPrefix: "legacy:nonce"}).keyPrefix},
        "token store":  {"legacy:token", NewTokenStore(connection, TokenStoreConfig{Prefix: "legacy:token"}).prefix},
        "locker":       {"legacy:lock:", NewLocker(connection, LockerConfig{KeyPrefix: "legacy:lock:"}).keyPrefix},
    } {
        if pair[0] != pair[1] {
            t.Fatalf("%s: expected the explicit name %q verbatim, got %q", name, pair[0], pair[1])
        }
    }

    backplane := NewServerSentEventBackplane(NewConnection(newTokenStoreClient(t), "shop"), melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), ServerSentEventBackplaneConfig{Channel: "legacy:sse"})
    defer backplane.CloseWithContext(context.Background())

    if "legacy:sse" != backplane.channel {
        t.Fatalf("expected the explicit channel verbatim, got %q", backplane.channel)
    }
}

func TestComponent_ANilConnectionIsRefusedNamingTheArgument(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    for name, build := range map[string]func(){
        "locker":       func() { NewLocker(nil, LockerConfig{}) },
        "token store":  func() { NewTokenStore(nil, TokenStoreConfig{}) },
        "rate limiter": func() { NewRateLimiter(nil, "login", 1, time.Second, RateLimiterConfig{}) },
        "nonce guard":  func() { NewNonceGuard(nil, NonceGuardConfig{}) },
        "backplane":    func() { NewServerSentEventBackplane(nil, hub, ServerSentEventBackplaneConfig{}) },
    } {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingTheArgument(t, "connection", build)
        })
    }
}
