package rueidis

import (
    "context"
    "net"
    "time"

    "github.com/precision-soft/melody/config"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/redis/rueidis"
)

func NewProvider(
    addressParameterName string,
    userParameterName string,
    passwordParameterName string,
) *Provider {
    return &Provider{
        addressParameterName:  addressParameterName,
        userParameterName:     userParameterName,
        passwordParameterName: passwordParameterName,
        clientConfig:          nil,
        timeoutConfig:         nil,
    }
}

func NewProviderWithConfig(
    addressParameterName string,
    userParameterName string,
    passwordParameterName string,
    clientConfig *ClientConfig,
    timeoutConfig *TimeoutConfig,
) *Provider {
    return &Provider{
        addressParameterName:  addressParameterName,
        userParameterName:     userParameterName,
        passwordParameterName: passwordParameterName,
        clientConfig:          clientConfig,
        timeoutConfig:         timeoutConfig,
    }
}

type Provider struct {
    addressParameterName  string
    userParameterName     string
    passwordParameterName string

    clientConfig  *ClientConfig
    timeoutConfig *TimeoutConfig
}

func (instance *Provider) WithClientConfig(clientConfig *ClientConfig) *Provider {
    instance.clientConfig = clientConfig

    return instance
}

func (instance *Provider) WithTimeoutConfig(timeoutConfig *TimeoutConfig) *Provider {
    instance.timeoutConfig = timeoutConfig

    return instance
}

func (instance *Provider) Open(resolver containercontract.Resolver) (rueidis.Client, error) {
    configuration := config.ConfigMustFromResolver(resolver)

    address := configuration.MustGet(instance.addressParameterName).MustString()
    user := configuration.MustGet(instance.userParameterName).String()
    password := configuration.MustGet(instance.passwordParameterName).String()

    connectionConfig := NewConnectionConfig(address, user, password)

    clientConfig := instance.clientConfig
    if nil == clientConfig {
        clientConfig = DefaultClientConfig()
    }

    timeoutConfig := instance.timeoutConfig
    if nil == timeoutConfig {
        timeoutConfig = DefaultTimeoutConfig()
    }

    addresses := parseAddressList(address)
    if 0 == len(addresses) {
        return nil, exception.NewError(
            "redis address is empty",
            connectionConfig.SafeContext(),
            nil,
        )
    }

    option := rueidis.ClientOption{
        InitAddress:  addresses,
        Username:     user,
        Password:     password,
        ClientName:   clientConfig.ClientName,
        SelectDB:     clientConfig.SelectDb,
        DisableCache: clientConfig.DisableCache,
        TLSConfig:    clientConfig.TlsConfig,
    }

    if 0 < clientConfig.DialTimeout {
        option.Dialer = net.Dialer{
            Timeout: clientConfig.DialTimeout,
        }
    }

    if 0 < clientConfig.ConnWriteTimeout {
        option.ConnWriteTimeout = clientConfig.ConnWriteTimeout
    }

    client, createErr := rueidis.NewClient(option)
    if nil != createErr {
        return nil, exception.NewError(
            "redis client creation failed",
            connectionConfig.SafeContext(),
            createErr,
        )
    }

    if false == clientConfig.PingOnStart {
        return client, nil
    }

    ctx, cancel := context.WithTimeout(context.Background(), timeoutConfig.ConnectTimeout)
    defer cancel()

    pingErr := client.Do(ctx, client.B().Ping().Build()).Error()
    if nil == pingErr {
        return client, nil
    }

    client.Close()

    return nil, exception.NewError(
        "redis connection failed",
        connectionConfig.SafeContext(),
        pingErr,
    )
}

func (instance *Provider) Close(client rueidis.Client) error {
    if nil == client {
        return nil
    }

    client.Close()
    return nil
}

func (instance *Provider) Ping(client rueidis.Client) error {
    if nil == client {
        return exception.NewError(
            "redis client is nil",
            nil,
            nil,
        )
    }

    commandTimeout := 3 * time.Second
    if nil != instance.timeoutConfig && 0 < instance.timeoutConfig.CommandTimeout {
        commandTimeout = instance.timeoutConfig.CommandTimeout
    }

    ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
    defer cancel()

    return client.Do(ctx, client.B().Ping().Build()).Error()
}
