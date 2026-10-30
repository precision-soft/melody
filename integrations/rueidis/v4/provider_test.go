package rueidis

import (
    "errors"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func TestNewProvider_TheZeroConfigResolvesTheDefaults(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

    if 3 != provider.retry.maxAttempts {
        t.Fatalf("expected an absent retry to be the default retry of three attempts, got %d", provider.retry.maxAttempts)
    }

    if 3*time.Second != provider.timeout.connectTimeout || 3*time.Second != provider.timeout.commandTimeout {
        t.Fatalf("expected the default ping deadlines, got %+v", provider.timeout)
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
        {field: "TimeoutConfig.ConnectTimeout", config: ProviderConfig{Timeout: TimeoutConfig{ConnectTimeout: bound.UnlimitedDuration()}}},
        {field: "RetryConfig.MaxAttempts", config: ProviderConfig{Retry: RetryConfig{MaxAttempts: bound.Of(0)}}},
    } {
        expectPanicNamingField(t, testCase.field, func() {
            NewProvider(testCase.config)
        })
    }
}

func TestProvider_ClientOptionTranslatesTheResolvedClient(t *testing.T) {
    option := NewProvider(ProviderConfig{}).clientOption([]string{"127.0.0.1:6379"}, ConnectionParameters{Address: "127.0.0.1:6379"})

    if 5*time.Second != option.Dialer.Timeout || 5*time.Second != option.ConnWriteTimeout {
        t.Fatalf("expected the default 5s dialer and write deadline installed, got %s / %s", option.Dialer.Timeout, option.ConnWriteTimeout)
    }

    if false == option.DisableCache || 0 != option.CacheSizeEachConn {
        t.Fatalf("expected the client-side cache off and no budget handed, got %v / %d", option.DisableCache, option.CacheSizeEachConn)
    }

    cached := NewProvider(ProviderConfig{Client: ClientConfig{ClientSideCacheEnabled: true}}).clientOption([]string{"127.0.0.1:6379"}, ConnectionParameters{})
    if true == cached.DisableCache || 128*(1<<20) != cached.CacheSizeEachConn {
        t.Fatalf("expected the client-side cache on with its 128 MiB budget, got %v / %d", cached.DisableCache, cached.CacheSizeEachConn)
    }

    budgeted := NewProvider(ProviderConfig{Client: ClientConfig{ClientSideCacheEnabled: true, ClientSideCacheBytes: bound.Of(1 << 20)}}).clientOption([]string{"127.0.0.1:6379"}, ConnectionParameters{})
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
            provider := NewProvider(ProviderConfig{})

            client, openErr := provider.Open(ConnectionParameters{Address: testCase.address})

            if nil == openErr {
                if nil != client {
                    client.Close()
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
    provider := NewProvider(ProviderConfig{
        Client:  ClientConfig{DialTimeout: bound.Of(250 * time.Millisecond), ConnWriteTimeout: bound.Of(250 * time.Millisecond)},
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(500 * time.Millisecond), CommandTimeout: bound.Of(500 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(1)},
    })

    client, openErr := provider.Open(ConnectionParameters{Address: "127.0.0.1:1", User: "app-user", Password: "s3cret-value"})

    if nil == openErr {
        if nil != client {
            client.Close()
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

func TestProvider_Close_NilClientReturnsNil(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

    closeErr := provider.Close(nil)
    if nil != closeErr {
        t.Fatalf("Close(nil) must be a no-op, got: %v", closeErr)
    }
}

func TestProvider_Ping_NilClientReturnsError(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

    pingErr := provider.Ping(nil)
    if nil == pingErr {
        t.Fatalf("Ping(nil) must return an error")
    }

    if false == strings.Contains(pingErr.Error(), "redis client is nil") {
        t.Fatalf("expected the nil-client error, got: %v", pingErr)
    }
}

func TestProvider_Open_AnAbsentConnectTimeoutPingsUnderTheDefaultBound(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis provider integration test")
    }

    provider := NewProvider(ProviderConfig{Timeout: TimeoutConfig{CommandTimeout: bound.Of(3 * time.Second)}})

    if 3*time.Second != provider.timeout.connectTimeout {
        t.Fatalf("an absent connect timeout must take the default bound, got %s", provider.timeout.connectTimeout)
    }

    client, openErr := provider.Open(ConnectionParameters{Address: address})
    if nil != openErr {
        t.Fatalf("open with an absent connect timeout against healthy redis: %v", openErr)
    }

    pingErr := provider.Ping(client)
    if nil != pingErr {
        t.Fatalf("ping: %v", pingErr)
    }

    closeErr := provider.Close(client)
    if nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }
}

/* the frozen majors also assert the configuration parameter the refusal names, because there the provider is told the parameter names and the operator needs the key to go and set. This major is handed the values, so the refusal names the deadlines that governed the attempt and nothing else — and above all not the credential. */
func TestProviderOpen_TheRefusalNamesTheDeadlinesAndNotTheCredential(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

    _, openErr := provider.Open(ConnectionParameters{User: "melody", Password: "secret"})
    if nil == openErr {
        t.Fatal("expected an empty address to be refused")
    }

    var melodyErr *exception.Error
    if false == errors.As(openErr, &melodyErr) {
        t.Fatalf("expected a melody error, got %T", openErr)
    }

    errorContext := melodyErr.Context()

    if _, exists := errorContext["connectTimeout"]; false == exists {
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
    connectionContext := NewProvider(ProviderConfig{}).connectionContext(ConnectionParameters{Address: "127.0.0.1:6379"})

    if "5s" != connectionContext["dialTimeout"] || "3s" != connectionContext["connectTimeout"] {
        t.Fatalf("expected 5s / 3s, got %v", connectionContext)
    }

    configured := NewProvider(ProviderConfig{
        Client:  ClientConfig{DialTimeout: bound.Of(2 * time.Second)},
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(7 * time.Second)},
    }).connectionContext(ConnectionParameters{Address: "127.0.0.1:6379"})

    if "2s" != configured["dialTimeout"] || "7s" != configured["connectTimeout"] {
        t.Fatalf("expected 2s / 7s, got %v", configured)
    }
}

func TestProvider_OpenWithRetry_ReportsEachRetryAndTheExhaustionOnTheLoggerItIsHanded(t *testing.T) {
    logger := &capturingLogger{}

    provider := NewProvider(ProviderConfig{
        Client:  ClientConfig{DialTimeout: bound.Of(250 * time.Millisecond), ConnWriteTimeout: bound.Of(250 * time.Millisecond)},
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(500 * time.Millisecond), CommandTimeout: bound.Of(500 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(2), InitialDelay: bound.Of(time.Millisecond), MaxDelay: bound.Of(time.Millisecond), BackoffMultiplier: 1},
        Logger:  logger,
    })

    client, openErr := provider.Open(ConnectionParameters{Address: "127.0.0.1:1", User: "app-user", Password: "s3cret-value"})

    if nil == openErr {
        if nil != client {
            client.Close()
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
    if logging.EmergencyLogger() != NewProvider(ProviderConfig{}).resolvedLogger() {
        t.Fatalf("expected the emergency logger when no logger is handed")
    }

    if logging.EmergencyLogger() != NewProvider(ProviderConfig{Logger: (*capturingLogger)(nil)}).resolvedLogger() {
        t.Fatalf("expected a typed-nil logger to be read as absent")
    }

    logger := &capturingLogger{}
    if logger != NewProvider(ProviderConfig{Logger: logger}).resolvedLogger() {
        t.Fatalf("expected the handed logger to be the one the retry reports in")
    }
}

func TestProvider_AnAbsentRetryDialsThreeTimesAndASingleAttemptOnce(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        retry    RetryConfig
        attempts int
    }{
        {name: "absent", retry: RetryConfig{InitialDelay: bound.Of(time.Millisecond), MaxDelay: bound.Of(time.Millisecond)}, attempts: 3},
        {name: "single", retry: RetryConfig{MaxAttempts: bound.Of(1)}, attempts: 1},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            logger := &capturingLogger{}

            provider := NewProvider(ProviderConfig{
                Client: ClientConfig{DialTimeout: bound.Of(250 * time.Millisecond)},
                Retry:  testCase.retry,
                Logger: logger,
            })

            client, openErr := provider.Open(ConnectionParameters{Address: "127.0.0.1:1"})
            if nil == openErr {
                client.Close()
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

    pinging := NewProvider(ProviderConfig{
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(200 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(1)},
        Logger:  &capturingLogger{},
    })

    client, openErr := pinging.Open(ConnectionParameters{Address: address})
    if nil == openErr {
        client.Close()
        t.Fatal("expected the zero client's boot ping to fail against a store that never answers it")
    }

    if false == strings.Contains(openErr.Error(), "redis connection failed") {
        t.Fatalf("expected the boot ping refusal, got %v", openErr)
    }

    silent := NewProvider(ProviderConfig{
        Client:  ClientConfig{BootPingDisabled: true},
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(200 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(1)},
    })

    client, openErr = silent.Open(ConnectionParameters{Address: address})
    if nil != openErr {
        t.Fatalf("expected BootPingDisabled to open without pinging, got %v", openErr)
    }

    client.Close()
}

func TestProvider_Ping_IsBoundedByTheCommandTimeout(t *testing.T) {
    address := pingSwallowingProxy(t)

    provider := NewProvider(ProviderConfig{
        Client:  ClientConfig{BootPingDisabled: true},
        Timeout: TimeoutConfig{ConnectTimeout: bound.Of(5 * time.Second), CommandTimeout: bound.Of(100 * time.Millisecond)},
        Retry:   RetryConfig{MaxAttempts: bound.Of(1)},
    })

    client, openErr := provider.Open(ConnectionParameters{Address: address})
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }

    defer client.Close()

    started := time.Now()
    pingErr := provider.Ping(client)
    if nil == pingErr {
        t.Fatal("expected a ping the store never answers to fail")
    }

    if elapsed := time.Since(started); 2*time.Second < elapsed {
        t.Fatalf("expected the 100ms command timeout to end the ping, it took %s", elapsed)
    }
}
