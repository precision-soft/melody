package rueidis

import (
    "context"
    "net"
    "reflect"
    "time"

    "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
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

/* SecretParameterNames names the configuration parameters that hold this provider's credentials, the capability shape the bunorm siblings expose to their registry; MarkSecretParameters reads it at wiring time. */
func (instance *Provider) SecretParameterNames() []string {
    return []string{instance.passwordParameterName}
}

/* MarkSecretParameters arms the framework's redaction for every credential parameter the providers name, at wiring rather than at the first dial, since debug:parameters never reaches the dial. The configuration is asked through the tolerant door, so a resolver without a configuration service leaves the marking undone and the call is safe from any wiring. */
func MarkSecretParameters(resolver containercontract.Resolver, providerList ...*Provider) {
    configuration, configurationErr := container.FromResolver[configcontract.Configuration](resolver, config.ServiceConfig)
    if nil != configurationErr || true == isNilInterface(configuration) {
        return
    }

    for _, provider := range providerList {
        if nil == provider {
            continue
        }

        for _, parameterName := range provider.SecretParameterNames() {
            if "" == parameterName {
                continue
            }

            configuration.MarkSecret(parameterName)
        }
    }
}

func (instance *Provider) Open(resolver containercontract.Resolver) (rueidis.Client, error) {
    configuration := config.ConfigMustFromResolver(resolver)

    /* the provider is told which parameter holds the credential, so it arms the framework's redaction for it and every template derived from it; this covers a process that reaches the dial, and MarkSecretParameters covers the rest at wiring */
    configuration.MarkSecret(instance.passwordParameterName)

    /* all three parameters read through MustString, as the bunorm siblings read theirs: a credential registered with the wrong type panics at boot naming the parameter and the type, where String() would fold it to "" and connect with no credential */
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
            instance.connectionContext(connectionConfig, clientConfig, timeoutConfig),
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
            instance.connectionContext(connectionConfig, clientConfig, timeoutConfig),
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
        instance.connectionContext(connectionConfig, clientConfig, timeoutConfig),
        pingErr,
    )
}

/* connectionContext is the diagnostic shape of every refusal this provider writes, assembled here because only the provider knows which configuration parameter the address was read from and which deadlines governed the attempt; it is the shape the bunorm siblings' toConnectionContext writes. The password is never part of it. */
func (instance *Provider) connectionContext(
    connectionConfig *ConnectionConfig,
    clientConfig *ClientConfig,
    timeoutConfig *TimeoutConfig,
) exceptioncontract.Context {
    connectionContext := connectionConfig.SafeContext()

    connectionContext["addressParameter"] = instance.addressParameterName
    connectionContext["userParameter"] = instance.userParameterName
    connectionContext["connectTimeout"] = resolveConnectTimeout(timeoutConfig).String()

    if nil != clientConfig {
        connectionContext["dialTimeout"] = resolveDialTimeoutDescription(clientConfig)
        connectionContext["selectDb"] = clientConfig.SelectDb
    }

    return connectionContext
}

/* libraryDefaultDialTimeout is rueidis's own, applied whenever this provider installs no dialer of its own. */
const libraryDefaultDialTimeout = 5 * time.Second

/* resolveDialTimeoutDescription reports the deadline that governed the dial, not the configured one: the custom dialer is installed only for a positive value, so a zero or negative DialTimeout runs under the library's own five seconds, and the record says so. */
func resolveDialTimeoutDescription(clientConfig *ClientConfig) string {
    if 0 < clientConfig.DialTimeout {
        return clientConfig.DialTimeout.String()
    }

    return libraryDefaultDialTimeout.String() + " (library default)"
}

/* resolveConnectTimeout bounds the boot ping. A non-positive value takes the default rather than removing the bound, as Ping and this package's options read theirs, since an unbounded ping against a store that never answers would hang boot holding a client no one can close yet. */
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

/* isNilInterface answers whether the interface value is nil or holds a nil pointer, map, slice, channel or function, since a typed nil passes a plain nil comparison and panics on first use; it duplicates the framework's internal helper, which a separate module cannot import. */
func isNilInterface(value any) bool {
    if nil == value {
        return true
    }

    reflected := reflect.ValueOf(value)

    switch reflected.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
        return reflected.IsNil()
    default:
        return false
    }
}
