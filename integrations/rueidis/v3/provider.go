package rueidis

import (
    "context"
    "net"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/redis/rueidis"
)

func NewProvider(
    options ...ProviderOption,
) *Provider {
    provider := &Provider{
        clientConfig:  nil,
        timeoutConfig: nil,
    }
    for _, option := range options {
        option(provider)
    }
    return provider
}

type ProviderOption func(*Provider)

func WithClientConfig(clientConfig *ClientConfig) ProviderOption {
    return func(p *Provider) {
        p.clientConfig = clientConfig
    }
}

func WithTimeoutConfig(timeoutConfig *TimeoutConfig) ProviderOption {
    return func(p *Provider) {
        p.timeoutConfig = timeoutConfig
    }
}

type Provider struct {
    clientConfig  *ClientConfig
    timeoutConfig *TimeoutConfig
}

func (instance *Provider) Open(params ConnectionParams) (rueidis.Client, error) {
    clientConfig := instance.clientConfig
    if nil == clientConfig {
        clientConfig = DefaultClientConfig()
    }

    timeoutConfig := instance.timeoutConfig
    if nil == timeoutConfig {
        timeoutConfig = DefaultTimeoutConfig()
    }

    addresses := parseAddressList(params.Address)
    if 0 == len(addresses) {
        return nil, exception.NewError(
            "redis address is empty",
            params.SafeContext(),
            nil,
        )
    }

    option := rueidis.ClientOption{
        InitAddress:  addresses,
        Username:     params.User,
        Password:     params.Password,
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
            params.SafeContext(),
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
        params.SafeContext(),
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
