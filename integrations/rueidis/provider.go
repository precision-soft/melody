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

    /* the provider is the component told authoritatively which parameter holds the credential, so it arms the framework's own redaction for it — the introspection output masks the password and every template derived from it, without the application repeating the knowledge */
    configuration.MarkSecret(instance.passwordParameterName)

    /* all three parameters read through MustString, the convention of the bunorm siblings: a credential registered with the wrong type panics at boot naming the parameter and the type, where String() would fold it to "" and connect with no credential at all — green against a passwordless store, "redis connection failed" pointing at the network against a secured one */
    address := configuration.MustGet(instance.addressParameterName).MustString()
    user := configuration.MustGet(instance.userParameterName).MustString()
    password := configuration.MustGet(instance.passwordParameterName).MustString()

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

    pingContext, pingCancel := context.WithTimeout(context.Background(), resolveConnectTimeout(timeoutConfig))
    defer pingCancel()

    pingErr := client.Do(pingContext, client.B().Ping().Build()).Error()
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

/* resolveConnectTimeout bounds the boot ping. A non-positive value takes the default rather than removing the bound, the way Ping reads its own zero and the way this package's options read theirs: a TimeoutConfig that names only the command timeout would otherwise run the ping on a context with no deadline, and a store that accepts the connection without answering would hang boot forever holding a client no one can close yet. */
func resolveConnectTimeout(timeoutConfig *TimeoutConfig) time.Duration {
    if nil == timeoutConfig || 0 >= timeoutConfig.ConnectTimeout {
        return DefaultTimeoutConfig().ConnectTimeout
    }

    return timeoutConfig.ConnectTimeout
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
