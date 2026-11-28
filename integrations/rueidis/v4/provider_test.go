package rueidis

import (
    "context"
    "crypto/tls"
    "errors"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "sync/atomic"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    redisclient "github.com/redis/rueidis"
)

func TestNewProvider_TheZeroConfigResolvesTheDefaults(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{})

    if 3 != provider.retry.maxAttempts {
        t.Fatalf("expected an absent retry to be the default retry of three attempts, got %d", provider.retry.maxAttempts)
    }

    if 3*time.Second != provider.client.bootPingTimeout {
        t.Fatalf("expected the default boot ping deadline, got %s", provider.client.bootPingTimeout)
    }

    if 5*time.Second != provider.client.dialTimeout || true == provider.client.bootPingDisabled || true == provider.client.clientSideCacheEnabled {
        t.Fatalf("expected the default client, got %+v", provider.client)
    }
}

func TestNewProvider_RefusesALimitOutsideItsRuleNamingTheField(t *testing.T) {
    for _, testCase := range []struct {
        field  string
        config ProviderConfig
    }{
        {field: "ClientConfig.DialTimeout", config: ProviderConfig{Client: ClientConfig{DialTimeout: bound.UnlimitedDuration()}}},
        {field: "ClientConfig.BootPingTimeout", config: ProviderConfig{Client: ClientConfig{BootPingTimeout: bound.UnlimitedDuration()}}},
        {field: "RetryConfig.MaxAttempts", config: ProviderConfig{Retry: RetryConfig{MaxAttempts: bound.Of(0)}}},
    } {
        expectPanicNamingField(t, testCase.field, func() {
            NewProvider("melody", testCase.config)
        })
    }
}

func TestProvider_ClientOptionTranslatesTheResolvedClient(t *testing.T) {
    option := NewProvider("melody", ProviderConfig{}).clientOption([]string{"127.0.0.1:6379"}, ConnectionParameters{Address: "127.0.0.1:6379"})

    if 5*time.Second != option.Dialer.Timeout || 5*time.Second != option.ConnWriteTimeout {
        t.Fatalf("expected the default 5s dialer and write deadline installed, got %s / %s", option.Dialer.Timeout, option.ConnWriteTimeout)
    }

    if false == option.DisableCache || 0 != option.CacheSizeEachConn {
        t.Fatalf("expected the client-side cache off and no budget handed, got %v / %d", option.DisableCache, option.CacheSizeEachConn)
    }

    cached := NewProvider("melody", ProviderConfig{Client: ClientConfig{ClientSideCacheEnabled: true}}).clientOption([]string{"127.0.0.1:6379"}, ConnectionParameters{})
    if true == cached.DisableCache || 128*(1<<20) != cached.CacheSizeEachConn {
        t.Fatalf("expected the client-side cache on with its 128 MiB budget, got %v / %d", cached.DisableCache, cached.CacheSizeEachConn)
    }

    budgeted := NewProvider("melody", ProviderConfig{Client: ClientConfig{ClientSideCacheEnabled: true, ClientSideCacheBytes: bound.Of(1 << 20)}}).clientOption([]string{"127.0.0.1:6379"}, ConnectionParameters{})
    if 1<<20 != budgeted.CacheSizeEachConn {
        t.Fatalf("expected the configured 1 MiB budget, got %d", budgeted.CacheSizeEachConn)
    }
}

func TestProvider_Open_EmptyAddressFails(t *testing.T) {
    testCases := []struct {
        name    string
        address string
    }{
        {name: "empty", address: ""},
        {name: "whitespace only", address: "   "},
        {name: "separators only", address: " , , "},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            provider := NewProvider("melody", ProviderConfig{})

            client, openErr := provider.Open(openRuntime(t), ConnectionParameters{Address: testCase.address})

            if nil == openErr {
                if nil != client {
                    client.CloseWithContext(t.Context())
                }

                t.Fatalf("expected an error for an empty redis address, got nil")
            }

            if nil != client {
                t.Fatalf("expected a nil client on error, got %T", client)
            }

            if false == strings.Contains(openErr.Error(), "redis address is empty") {
                t.Fatalf("expected the empty-address error, got: %v", openErr)
            }
        })
    }
}

func TestProvider_Open_UnreachableAddressFailsWithoutLeakingPassword(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{
        Client: ClientConfig{DialTimeout: bound.Of(250 * time.Millisecond), ConnectionWriteTimeout: bound.Of(250 * time.Millisecond), BootPingTimeout: bound.Of(500 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(1)},
    })

    client, openErr := provider.Open(openRuntime(t), ConnectionParameters{Address: "127.0.0.1:1", User: "app-user", Password: "s3cret-value"})

    if nil == openErr {
        if nil != client {
            client.CloseWithContext(t.Context())
        }

        t.Fatalf("expected an error for an unreachable redis address, got nil")
    }

    if nil != client {
        t.Fatalf("expected a nil client on error, got %T", client)
    }

    if true == strings.Contains(openErr.Error(), "s3cret-value") {
        t.Fatalf("open error must not leak the password: %v", openErr)
    }
}

func TestProvider_Ping_ANilConnectionReturnsError(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{})

    for _, connection := range []*Connection{nil, {}} {
        pingErr := provider.Ping(openRuntime(t), connection)
        if nil == pingErr || false == strings.Contains(pingErr.Error(), "redis connection is nil") {
            t.Fatalf("expected the nil-connection error, got: %v", pingErr)
        }
    }
}

func TestProvider_Open_AnAbsentBootPingTimeoutPingsUnderTheDefaultBound(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis provider integration test")
    }

    provider := NewProvider("melody", ProviderConfig{})

    connection, openErr := provider.Open(openRuntime(t), ConnectionParameters{Address: address})
    if nil != openErr {
        t.Fatalf("open with an absent boot ping timeout against healthy redis: %v", openErr)
    }

    if pingErr := provider.Ping(openRuntime(t), connection); nil != pingErr {
        t.Fatalf("ping: %v", pingErr)
    }

    if closeErr := connection.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }
}

func TestProviderOpen_TheRefusalNamesTheDeadlinesAndNotTheCredential(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{})

    _, openErr := provider.Open(openRuntime(t), ConnectionParameters{User: "melody", Password: "secret"})
    if nil == openErr {
        t.Fatal("expected an empty address to be refused")
    }

    var melodyErr *exception.Error
    if false == errors.As(openErr, &melodyErr) {
        t.Fatalf("expected a melody error, got %T", openErr)
    }

    errorContext := melodyErr.Context()

    if _, exists := errorContext["bootPingTimeout"]; false == exists {
        t.Fatalf("expected the deadline that governs the attempt, got %v", errorContext)
    }

    if _, exists := errorContext["dialTimeout"]; false == exists {
        t.Fatalf("expected the dial deadline that governs the attempt, got %v", errorContext)
    }

    if "secret" == errorContext["password"] {
        t.Fatalf("expected the credential to stay out of the record, got %v", errorContext)
    }

    for key, value := range errorContext {
        if "secret" == value {
            t.Fatalf("the refusal leaks the credential under key %q", key)
        }
    }
}

func TestProvider_TheConnectionContextCarriesTheGoverningDeadlines(t *testing.T) {
    connectionContext := NewProvider("melody", ProviderConfig{}).connectionContext(ConnectionParameters{Address: "127.0.0.1:6379"})

    if "5s" != connectionContext["dialTimeout"] || "3s" != connectionContext["bootPingTimeout"] {
        t.Fatalf("expected 5s / 3s, got %v", connectionContext)
    }

    configured := NewProvider("melody", ProviderConfig{
        Client: ClientConfig{DialTimeout: bound.Of(2 * time.Second), BootPingTimeout: bound.Of(7 * time.Second)},
    }).connectionContext(ConnectionParameters{Address: "127.0.0.1:6379"})

    if "2s" != configured["dialTimeout"] || "7s" != configured["bootPingTimeout"] {
        t.Fatalf("expected 2s / 7s, got %v", configured)
    }

    selected := NewProvider("melody", ProviderConfig{Client: ClientConfig{SelectDatabase: 2}}).connectionContext(ConnectionParameters{Address: "127.0.0.1:6379"})
    if 2 != selected["selectDatabase"] {
        t.Fatalf("expected the database named under selectDatabase, got %v", selected)
    }
}

func TestProvider_OpenWithRetry_ReportsEachRetryAndTheExhaustionOnTheLoggerItIsHanded(t *testing.T) {
    logger := &capturingLogger{}

    provider := NewProvider("melody", ProviderConfig{
        Client: ClientConfig{DialTimeout: bound.Of(250 * time.Millisecond), ConnectionWriteTimeout: bound.Of(250 * time.Millisecond), BootPingTimeout: bound.Of(500 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(2), Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond), Max: bound.Of(time.Millisecond), Factor: 1}},
        Logger:  logger,
    })

    client, openErr := provider.Open(openRuntime(t), ConnectionParameters{Address: "127.0.0.1:1", User: "app-user", Password: "s3cret-value"})

    if nil == openErr {
        if nil != client {
            client.CloseWithContext(t.Context())
        }

        t.Fatalf("expected an error for an unreachable redis address, got nil")
    }

    if 2 != len(logger.records) {
        t.Fatalf("expected one retry warning and one exhaustion error, got %d record(s): %+v", len(logger.records), logger.records)
    }

    if loggingcontract.LevelWarning != logger.records[0].level || "redis connection failed and retrying" != logger.records[0].message {
        t.Fatalf("expected the first record to be the retry warning, got %+v", logger.records[0])
    }

    if loggingcontract.LevelError != logger.records[1].level || "redis connection failed after max retry attempts" != logger.records[1].message {
        t.Fatalf("expected the second record to be the exhaustion error, got %+v", logger.records[1])
    }
}

func TestProvider_WithoutALoggerTheRetryReportsReachTheEmergencyLogger(t *testing.T) {
    emergencyDefault, _ := logging.ResolveOptionalLogger(nil, "test", "Test.Logger")
    if emergencyDefault != NewProvider("melody", ProviderConfig{}).resolvedLogger() {
        t.Fatalf("expected the emergency logger when no logger is handed")
    }

    expectPanicNamingField(t, "ProviderConfig.Logger", func() {
        NewProvider("melody", ProviderConfig{Logger: (*capturingLogger)(nil)})
    })

    logger := &capturingLogger{}
    if logger != NewProvider("melody", ProviderConfig{Logger: logger}).resolvedLogger() {
        t.Fatalf("expected the handed logger to be the one the retry reports in")
    }
}

func TestProvider_AnAbsentRetryDialsThreeTimesAndASingleAttemptOnce(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        retry    RetryConfig
        attempts int
    }{
        {name: "absent", retry: RetryConfig{Backoff: bound.Backoff{Initial: bound.Of(time.Millisecond), Max: bound.Of(time.Millisecond)}}, attempts: 3},
        {name: "single", retry: RetryConfig{MaxAttempts: bound.Of(1)}, attempts: 1},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            logger := &capturingLogger{}

            provider := NewProvider("melody", ProviderConfig{
                Client: ClientConfig{DialTimeout: bound.Of(250 * time.Millisecond)},
                Retry:  testCase.retry,
                Logger: logger,
            })

            client, openErr := provider.Open(openRuntime(t), ConnectionParameters{Address: "127.0.0.1:1"})
            if nil == openErr {
                client.CloseWithContext(t.Context())
                t.Fatal("expected an unreachable address to be refused")
            }

            if testCase.attempts != len(logger.records) {
                t.Fatalf("expected %d record(s), one per attempt, got %+v", testCase.attempts, logger.records)
            }

            last := logger.records[len(logger.records)-1]
            if loggingcontract.LevelError != last.level || testCase.attempts != last.context["attempt"] {
                t.Fatalf("expected the terminal failure journaled at attempt %d, got %+v", testCase.attempts, last)
            }
        })
    }
}

func TestProvider_Open_TheZeroClientPingsOnStartAndBootPingDisabledSkipsIt(t *testing.T) {
    address := pingSwallowingProxy(t)

    pinging := NewProvider("melody", ProviderConfig{
        Client:  ClientConfig{BootPingTimeout: bound.Of(200 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(1)},
        Logger:  &capturingLogger{},
    })

    client, openErr := pinging.Open(openRuntime(t), ConnectionParameters{Address: address})
    if nil == openErr {
        client.CloseWithContext(t.Context())
        t.Fatal("expected the zero client's boot ping to fail against a store that never answers it")
    }

    if false == strings.Contains(openErr.Error(), "redis connection failed") {
        t.Fatalf("expected the boot ping refusal, got %v", openErr)
    }

    silent := NewProvider("melody", ProviderConfig{
        Client:  ClientConfig{BootPingDisabled: true, BootPingTimeout: bound.Of(200 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(1)},
    })

    client, openErr = silent.Open(openRuntime(t), ConnectionParameters{Address: address})
    if nil != openErr {
        t.Fatalf("expected BootPingDisabled to open without pinging, got %v", openErr)
    }

    client.CloseWithContext(t.Context())
}

func TestProvider_OpenReturnsOnACyclicChainFromATlsVerifier(t *testing.T) {
    server := httptest.NewTLSServer(nethttp.HandlerFunc(func(nethttp.ResponseWriter, *nethttp.Request) {}))
    defer server.Close()

    first := &rateLimiterCyclicError{}
    first.next = &rateLimiterCyclicError{next: first}

    provider := NewProvider("melody", ProviderConfig{
        Client: ClientConfig{
            TlsConfig: &tls.Config{
                InsecureSkipVerify: true,
                VerifyConnection: func(tls.ConnectionState) error {
                    return first
                },
            },
        },
        Retry: RetryConfig{MaxAttempts: bound.Of(1)},
    })

    done := make(chan struct{})
    go func() {
        defer close(done)

        if _, openErr := provider.Open(openRuntime(t), ConnectionParameters{Address: server.Listener.Addr().String()}); nil == openErr {
            t.Error("expected the verifier's refusal")
        }
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the open to return on an error chain that reaches itself")
    }

    if true == provider.isTransientError(first) {
        t.Fatal("expected a chain with no transient text not to read as transient")
    }
}

func countingListener(t *testing.T) (string, *atomic.Int64) {
    t.Helper()

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }

    t.Cleanup(func() {
        listener.Close()
    })

    accepted := &atomic.Int64{}

    go func() {
        for {
            connection, acceptErr := listener.Accept()
            if nil != acceptErr {
                return
            }

            accepted.Add(1)
            connection.Close()
        }
    }()

    return listener.Addr().String(), accepted
}

type doneContextCause struct{}

func (doneContextCause) Error() string {
    return "the caller gave up"
}

func TestProviderOpen_ADoneContextRefusesBeforeTheClientIsBuilt(t *testing.T) {
    address, accepted := countingListener(t)

    openContext, cancel := context.WithCancelCause(t.Context())
    cancel(doneContextCause{})

    provider := NewProvider("melody", ProviderConfig{Retry: RetryConfig{MaxAttempts: bound.Of(1)}, Logger: &capturingLogger{}})

    connection, openErr := provider.Open(runtimeOver(openContext), ConnectionParameters{Address: address})
    if nil != connection || nil == openErr {
        t.Fatalf("expected a done context refused, got %v, %v", connection, openErr)
    }

    if false == strings.Contains(openErr.Error(), "the caller's context is done") || false == errors.Is(openErr, doneContextCause{}) {
        t.Fatalf("expected the refusal to carry the context's cause, got %v", openErr)
    }

    time.Sleep(100 * time.Millisecond)

    if 0 != accepted.Load() {
        t.Fatalf("expected the address never dialled, it was dialled %d time(s)", accepted.Load())
    }
}

func TestProviderOpen_TheRetryWaitEndsWithTheContext(t *testing.T) {
    logger := &capturingLogger{}

    provider := NewProvider("melody", ProviderConfig{
        Client: ClientConfig{DialTimeout: bound.Of(250 * time.Millisecond)},
        Retry:  RetryConfig{MaxAttempts: bound.Of(10), Backoff: bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(5 * time.Second)}},
        Logger: logger,
    })

    openContext, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
    defer cancel()

    started := time.Now()
    connection, openErr := provider.Open(runtimeOver(openContext), ConnectionParameters{Address: "127.0.0.1:1"})
    elapsed := time.Since(started)

    if nil != connection || nil == openErr {
        t.Fatalf("expected the open refused, got %v, %v", connection, openErr)
    }

    if 900*time.Millisecond < elapsed {
        t.Fatalf("expected the wait to end with the context, it took %s", elapsed)
    }

    if false == strings.Contains(openErr.Error(), "redis connection retry ended by the caller's context") || false == errors.Is(openErr, context.DeadlineExceeded) {
        t.Fatalf("expected the wait's refusal with the context's cause, got %v", openErr)
    }

    errorContext := exceptionContextOf(openErr)
    if 1 != errorContext["attempt"] || 10 != errorContext["maxAttempts"] {
        t.Fatalf("expected attempt 1 of 10, got %v", errorContext)
    }

    if lastError, isString := errorContext["lastError"].(string); false == isString || "" == lastError {
        t.Fatalf("expected the last dial error, got %v", errorContext)
    }
}

func TestProviderOpen_TheLastErrorIsBounded(t *testing.T) {
    long := strings.Repeat("é", 400)

    bounded := boundLastError(long)
    if 600 < len(bounded) || false == strings.Contains(bounded, "...(truncated ") || false == utf8.ValidString(bounded) {
        t.Fatalf("expected a bounded, valid text, got %d bytes", len(bounded))
    }

    if "short" != boundLastError("short") {
        t.Fatalf("expected a short text kept whole")
    }
}

func TestProviderOpen_TheBootPingIsBoundedWithinTheCallersContext(t *testing.T) {
    address := pingSwallowingProxy(t)

    t.Run("the caller's earlier deadline ends it", func(t *testing.T) {
        provider := NewProvider("melody", ProviderConfig{
            Client: ClientConfig{BootPingTimeout: bound.Of(5 * time.Second)},
            Retry:  RetryConfig{MaxAttempts: bound.Of(1)},
            Logger: &capturingLogger{},
        })

        openContext, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
        defer cancel()

        openErr := awaitOutcome(t, boundProbeBudget, func() error {
            _, err := provider.Open(runtimeOver(openContext), ConnectionParameters{Address: address})

            return err
        })
        requireDeadlineExceeded(t, openErr)
    })

    t.Run("the boot ping timeout governs inside a longer context", func(t *testing.T) {
        provider := NewProvider("melody", ProviderConfig{
            Client: ClientConfig{BootPingTimeout: bound.Of(200 * time.Millisecond)},
            Retry:  RetryConfig{MaxAttempts: bound.Of(1)},
            Logger: &capturingLogger{},
        })

        openErr := awaitOutcome(t, boundProbeBudget, func() error {
            _, err := provider.Open(openRuntime(t), ConnectionParameters{Address: address})

            return err
        })
        requireDeadlineExceeded(t, openErr)
    })
}

func TestProviderOpen_AnswersTheOwnerThatClosesItsClientOnce(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis provider integration test")
    }

    connection, openErr := NewProvider("melody", ProviderConfig{}).Open(openRuntime(t), ConnectionParameters{Address: address})
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }

    client := connection.Client()
    if nil == client {
        t.Fatal("expected the owner to hold the client it opened")
    }

    if doErr := client.Do(t.Context(), client.B().Ping().Build()).Error(); nil != doErr {
        t.Fatalf("expected the opened client to answer, got %v", doErr)
    }

    for range 2 {
        if closeErr := connection.CloseWithContext(t.Context()); nil != closeErr {
            t.Fatalf("close: %v", closeErr)
        }
    }

    if doErr := client.Do(t.Context(), client.B().Ping().Build()).Error(); false == errors.Is(doErr, redisclient.ErrClosing) {
        t.Fatalf("expected the owner's close to close the client it opened, got %v", doErr)
    }
}

type contextRecordingClient struct {
    redisclient.Client
    seen context.Context
}

func (instance *contextRecordingClient) Do(ctx context.Context, command redisclient.Completed) redisclient.RedisResult {
    instance.seen = ctx

    return instance.Client.Do(ctx, command)
}

func TestProviderPing_RunsUnderTheCallersContextAlone(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis provider integration test")
    }

    provider := NewProvider("melody", ProviderConfig{})

    opened, openErr := provider.Open(openRuntime(t), ConnectionParameters{Address: address})
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }

    defer opened.CloseWithContext(context.Background())

    recording := &contextRecordingClient{Client: opened.Client()}
    pingContext := context.WithValue(t.Context(), doneContextCause{}, "the caller's")

    if pingErr := provider.Ping(runtimeOver(pingContext), NewConnection(recording, "melody")); nil != pingErr {
        t.Fatalf("ping: %v", pingErr)
    }

    if pingContext != recording.seen {
        t.Fatalf("expected the round trip to run under the caller's context as it is")
    }

    if _, hasDeadline := recording.seen.Deadline(); true == hasDeadline {
        t.Fatalf("expected no private deadline on the ping")
    }
}

func TestProviderOpen_ANilRuntimeIsRefusedNamingTheDoor(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{})

    _, openErr := provider.Open(nil, ConnectionParameters{Address: "127.0.0.1:1"})
    if nil == openErr || "rueidis.Provider.Open" != exceptionContextOf(openErr)["door"] {
        t.Fatalf("expected the open door named, got %v", openErr)
    }

    pingErr := provider.Ping(nil, NewConnection(&stubClient{}, "melody"))
    if nil == pingErr || "rueidis.Provider.Ping" != exceptionContextOf(pingErr)["door"] {
        t.Fatalf("expected the ping door named, got %v", pingErr)
    }
}

func TestNewProvider_AnEmptyNamespaceIsRefused(t *testing.T) {
    for _, namespace := range []string{"", "a:b", "Shop"} {
        expectPanicNamingTheArgument(t, "namespace", func() {
            NewProvider(namespace, ProviderConfig{})
        })
    }
}

func TestProvider_OpenHandsTheNamespaceToTheConnection(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS is not set")
    }

    connection, openErr := NewProvider("shop", ProviderConfig{}).Open(openRuntime(t), ConnectionParameters{Address: address})
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    defer connection.CloseWithContext(context.Background())

    if "shop" != connection.Namespace() {
        t.Fatalf("expected the provider's namespace on the connection, got %q", connection.Namespace())
    }
}

func TestOptionalDependencies_NilIsTheDefaultAndATypedNilIsRefused(t *testing.T) {
    client := newTokenStoreClient(t)

    rows := []struct {
        name  string
        field string
        build func(typedNil bool)
    }{
        {"NewProvider", "ProviderConfig.Logger", func(typedNil bool) {
            config := ProviderConfig{}
            if true == typedNil {
                config.Logger = (*capturingLogger)(nil)
            }

            NewProvider("melody", config)
        }},
        {"NewServerSentEventBackplane", "ServerSentEventBackplaneConfig.Logger", func(typedNil bool) {
            config := ServerSentEventBackplaneConfig{}
            if true == typedNil {
                config.Logger = (*capturingLogger)(nil)
            }

            backplane := NewServerSentEventBackplane(newTestConnection(client), melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), config)
            _ = backplane.CloseWithContext(context.Background())
        }},
        {"NewTokenStore", "TokenStoreConfig.Clock", func(typedNil bool) {
            config := TokenStoreConfig{}
            if true == typedNil {
                config.Clock = (*clock.FrozenClock)(nil)
            }

            NewTokenStore(newTestConnection(client), config)
        }},
    }

    for _, row := range rows {
        t.Run(row.name, func(t *testing.T) {
            row.build(false)

            expectPanicNamingField(t, row.field, func() { row.build(true) })
        })
    }
}
