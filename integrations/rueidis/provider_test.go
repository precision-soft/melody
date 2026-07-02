package rueidis

import (
    "os"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
)

const (
    testAddressParameterName  = "redis.address"
    testUserParameterName     = "redis.user"
    testPasswordParameterName = "redis.password"
)

type testEnvironmentSource struct {
    values map[string]string
}

func (instance *testEnvironmentSource) Load() (map[string]string, error) {
    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}

/* newProviderTestResolver builds a container whose service.config exposes the three redis connection values as runtime parameters, mirroring how an application feeds the config-parameter-driven provider */
func newProviderTestResolver(t *testing.T, address string, user string, password string) containercontract.Resolver {
    t.Helper()

    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            environment, environmentErr := config.NewEnvironment(
                &testEnvironmentSource{
                    values: map[string]string{
                        config.EnvKey: config.EnvDevelopment,
                    },
                },
            )
            if nil != environmentErr {
                return nil, environmentErr
            }

            configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
            if nil != configurationErr {
                return nil, configurationErr
            }

            configuration.RegisterRuntime(testAddressParameterName, address)
            configuration.RegisterRuntime(testUserParameterName, user)
            configuration.RegisterRuntime(testPasswordParameterName, password)

            return configuration, nil
        },
    )

    return serviceContainer
}

func newTestProvider() *Provider {
    return NewProvider(testAddressParameterName, testUserParameterName, testPasswordParameterName)
}

func TestNewProvider_StoresParameterNamesAndLeavesConfigsNil(t *testing.T) {
    provider := newTestProvider()

    if testAddressParameterName != provider.addressParameterName {
        t.Fatalf("address parameter name: expected %q, got %q", testAddressParameterName, provider.addressParameterName)
    }

    if testUserParameterName != provider.userParameterName {
        t.Fatalf("user parameter name: expected %q, got %q", testUserParameterName, provider.userParameterName)
    }

    if testPasswordParameterName != provider.passwordParameterName {
        t.Fatalf("password parameter name: expected %q, got %q", testPasswordParameterName, provider.passwordParameterName)
    }

    if nil != provider.clientConfig {
        t.Fatalf("client config must stay nil so Open falls back to DefaultClientConfig")
    }

    if nil != provider.timeoutConfig {
        t.Fatalf("timeout config must stay nil so Open falls back to DefaultTimeoutConfig")
    }
}

func TestNewProviderWithConfig_StoresProvidedConfigs(t *testing.T) {
    clientConfig := DefaultClientConfig()
    timeoutConfig := DefaultTimeoutConfig()

    provider := NewProviderWithConfig(
        testAddressParameterName,
        testUserParameterName,
        testPasswordParameterName,
        clientConfig,
        timeoutConfig,
    )

    if clientConfig != provider.clientConfig {
        t.Fatalf("client config must be stored as provided")
    }

    if timeoutConfig != provider.timeoutConfig {
        t.Fatalf("timeout config must be stored as provided")
    }
}

func TestProvider_WithClientConfig_SetsConfigAndReturnsSameInstance(t *testing.T) {
    provider := newTestProvider()
    clientConfig := DefaultClientConfig()

    returned := provider.WithClientConfig(clientConfig)

    if provider != returned {
        t.Fatalf("WithClientConfig must return the same provider instance for chaining")
    }

    if clientConfig != provider.clientConfig {
        t.Fatalf("WithClientConfig must store the provided client config")
    }
}

func TestProvider_WithTimeoutConfig_SetsConfigAndReturnsSameInstance(t *testing.T) {
    provider := newTestProvider()
    timeoutConfig := DefaultTimeoutConfig()

    returned := provider.WithTimeoutConfig(timeoutConfig)

    if provider != returned {
        t.Fatalf("WithTimeoutConfig must return the same provider instance for chaining")
    }

    if timeoutConfig != provider.timeoutConfig {
        t.Fatalf("WithTimeoutConfig must store the provided timeout config")
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
            provider := newTestProvider()

            client, openErr := provider.Open(newProviderTestResolver(t, testCase.address, "", ""))

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
    provider := NewProviderWithConfig(
        testAddressParameterName,
        testUserParameterName,
        testPasswordParameterName,
        &ClientConfig{
            ClientName:       "",
            SelectDb:         0,
            DisableCache:     true,
            TlsConfig:        nil,
            PingOnStart:      true,
            DialTimeout:      250 * time.Millisecond,
            ConnWriteTimeout: 250 * time.Millisecond,
        },
        &TimeoutConfig{
            ConnectTimeout: 500 * time.Millisecond,
            CommandTimeout: 500 * time.Millisecond,
        },
    )

    client, openErr := provider.Open(newProviderTestResolver(t, "127.0.0.1:1", "app-user", "s3cret-value"))

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
    provider := newTestProvider()

    closeErr := provider.Close(nil)
    if nil != closeErr {
        t.Fatalf("Close(nil) must be a no-op, got: %v", closeErr)
    }
}

func TestProvider_Ping_NilClientReturnsError(t *testing.T) {
    provider := newTestProvider()

    pingErr := provider.Ping(nil)
    if nil == pingErr {
        t.Fatalf("Ping(nil) must return an error")
    }

    if false == strings.Contains(pingErr.Error(), "redis client is nil") {
        t.Fatalf("expected the nil-client error, got: %v", pingErr)
    }
}

func TestProvider_Open_ZeroConnectTimeoutPingsWithoutDeadline(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis provider integration test")
    }

    provider := newTestProvider().WithTimeoutConfig(
        &TimeoutConfig{
            ConnectTimeout: 0,
            CommandTimeout: 3 * time.Second,
        },
    )

    client, openErr := provider.Open(newProviderTestResolver(t, address, "", ""))
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
