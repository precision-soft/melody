package rueidis

import (
    "errors"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

func TestNewProvider_LeavesEveryConfigNilSoOpenFallsBackToTheDefaults(t *testing.T) {
    provider := NewProvider()

    if nil != provider.clientConfig {
        t.Fatalf("client config must stay nil so Open falls back to DefaultClientConfig")
    }

    if nil != provider.timeoutConfig {
        t.Fatalf("timeout config must stay nil so Open falls back to DefaultTimeoutConfig")
    }

    if nil != provider.retryConfig {
        t.Fatalf("retry config must stay nil so Open dials once instead of entering the retry loop")
    }
}

func TestProviderOptions_StoreTheConfigurationsTheyAreGiven(t *testing.T) {
    clientConfig := DefaultClientConfig()
    timeoutConfig := DefaultTimeoutConfig()
    retryConfig := DefaultRetryConfig()

    provider := NewProvider(
        WithClientConfig(clientConfig),
        WithTimeoutConfig(timeoutConfig),
        WithRetryConfig(retryConfig),
    )

    if clientConfig != provider.clientConfig {
        t.Fatalf("client config must be stored as provided")
    }

    if timeoutConfig != provider.timeoutConfig {
        t.Fatalf("timeout config must be stored as provided")
    }

    if retryConfig != provider.retryConfig {
        t.Fatalf("retry config must be stored as provided")
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
            provider := NewProvider()

            client, openErr := provider.Open(NewConnectionParameters(testCase.address, "", ""))

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
    provider := NewProvider(
        WithClientConfig(
            &ClientConfig{
                ClientName:       "",
                SelectDb:         0,
                DisableCache:     true,
                TlsConfig:        nil,
                PingOnStart:      true,
                DialTimeout:      250 * time.Millisecond,
                ConnWriteTimeout: 250 * time.Millisecond,
            },
        ),
        WithTimeoutConfig(
            &TimeoutConfig{
                ConnectTimeout: 500 * time.Millisecond,
                CommandTimeout: 500 * time.Millisecond,
            },
        ),
    )

    client, openErr := provider.Open(NewConnectionParameters("127.0.0.1:1", "app-user", "s3cret-value"))

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
    provider := NewProvider()

    closeErr := provider.Close(nil)
    if nil != closeErr {
        t.Fatalf("Close(nil) must be a no-op, got: %v", closeErr)
    }
}

func TestProvider_Ping_NilClientReturnsError(t *testing.T) {
    provider := NewProvider()

    pingErr := provider.Ping(nil)
    if nil == pingErr {
        t.Fatalf("Ping(nil) must return an error")
    }

    if false == strings.Contains(pingErr.Error(), "redis client is nil") {
        t.Fatalf("expected the nil-client error, got: %v", pingErr)
    }
}

func TestProvider_Open_AZeroConnectTimeoutPingsUnderTheDefaultBound(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis provider integration test")
    }

    timeoutConfig := &TimeoutConfig{
        ConnectTimeout: 0,
        CommandTimeout: 3 * time.Second,
    }

    if DefaultTimeoutConfig().ConnectTimeout != resolveConnectTimeout(timeoutConfig) {
        t.Fatalf("a zero connect timeout must take the default bound, got %s", resolveConnectTimeout(timeoutConfig))
    }

    provider := NewProvider(WithTimeoutConfig(timeoutConfig))

    client, openErr := provider.Open(NewConnectionParameters(address, "", ""))
    if nil != openErr {
        t.Fatalf("open with zero connect timeout against healthy redis: %v", openErr)
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

func TestResolveConnectTimeout_ANonPositiveValueTakesTheDefaultRatherThanRemovingTheBound(t *testing.T) {
    defaultConnectTimeout := DefaultTimeoutConfig().ConnectTimeout

    if defaultConnectTimeout != resolveConnectTimeout(nil) {
        t.Fatalf("a missing config must take the default, got %s", resolveConnectTimeout(nil))
    }

    if defaultConnectTimeout != resolveConnectTimeout(&TimeoutConfig{CommandTimeout: 5 * time.Second}) {
        t.Fatal("a config naming only the command timeout must still bound the ping")
    }

    if defaultConnectTimeout != resolveConnectTimeout(&TimeoutConfig{ConnectTimeout: -time.Second}) {
        t.Fatal("a negative connect timeout must take the default rather than build an already-lapsed context")
    }

    if 7*time.Second != resolveConnectTimeout(&TimeoutConfig{ConnectTimeout: 7 * time.Second}) {
        t.Fatal("a configured connect timeout must govern")
    }
}

func TestProviderOpen_TheRefusalNamesTheDeadlinesAndNotTheCredential(t *testing.T) {
    provider := NewProvider()

    _, openErr := provider.Open(NewConnectionParameters("", "melody", "secret"))
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

func TestProvider_TheReportedDialTimeoutIsTheOneThatGovernedTheDial(t *testing.T) {
    for _, testCase := range []struct {
        name        string
        dialTimeout time.Duration
        expected    string
    }{
        {name: "a configured timeout is reported as itself", dialTimeout: 2 * time.Second, expected: "2s"},
        {name: "zero is the library default, and says so", dialTimeout: 0, expected: "5s (library default)"},
        {name: "a negative value is the library default too", dialTimeout: -1 * time.Second, expected: "5s (library default)"},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            clientConfig := DefaultClientConfig()
            clientConfig.DialTimeout = testCase.dialTimeout

            if testCase.expected != resolveDialTimeoutDescription(clientConfig) {
                t.Fatalf("dialTimeout = %q, want %q", resolveDialTimeoutDescription(clientConfig), testCase.expected)
            }
        })
    }
}

func TestProvider_TheConnectionContextCarriesTheGoverningDialTimeout(t *testing.T) {
    clientConfig := DefaultClientConfig()
    clientConfig.DialTimeout = 0

    connectionContext := NewProvider().connectionContext(
        NewConnectionParameters("127.0.0.1:6379", "", ""),
        clientConfig,
        DefaultTimeoutConfig(),
    )

    if "5s (library default)" != connectionContext["dialTimeout"] {
        t.Fatalf("dialTimeout = %v, want the governing value", connectionContext["dialTimeout"])
    }
}
