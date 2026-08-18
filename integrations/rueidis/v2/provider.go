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

/* SecretParameterNames names the configuration parameters that hold this provider's credentials, the capability shape the bunorm siblings expose to their registry. This integration has no registry to ask it at construction, so MarkSecretParameters is the door that reads it at wiring time. */
func (instance *Provider) SecretParameterNames() []string {
    return []string{instance.passwordParameterName}
}

/* MarkSecretParameters arms the framework's redaction for every credential parameter the providers name, at wiring rather than at the first dial: the marking inside Open covers only a process that reaches the dial, and debug:parameters is precisely the process that does not. The configuration is asked through the tolerant door — a resolver that carries no configuration service leaves the marking undone, the same way MarkSecret leaves an absent parameter alone — so the call is safe from any wiring, including one that builds no configuration at all. */
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

    /* the provider is the component told authoritatively which parameter holds the credential, so it arms the framework's own redaction for it — the introspection output masks the password and every template derived from it, without the application repeating the knowledge. This covers only a process that reaches the dial; the process that never does — a console run, debug:parameters above all — is covered by MarkSecretParameters, called at wiring. */
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

/* connectionContext is the diagnostic shape of every refusal this provider writes, and it is assembled here rather than inside ConnectionConfig.SafeContext because only the provider knows the two things the operator is missing: which configuration parameter the address was read from — "redis address is empty" names a package otherwise, not a key to go and set — and the deadlines that actually governed the attempt, which live in the client and timeout configurations the connection values know nothing about. It is the shape the bunorm siblings' toConnectionContext already writes for a failed dial. The password is never part of it: the safe context decides what may be rendered. */
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

/* resolveDialTimeoutDescription reports the deadline that GOVERNED the dial, not the one that was configured, which is what the rest of this context already does one line above for the connect timeout. The custom dialer is installed only for a positive value, so a zero or negative DialTimeout — the footgun of a partial ClientConfig literal — ran under the library's own five seconds while the record said "0s". An operator reads that as no dial bound at all and goes looking for a deadline that never existed; measured, the dial failed after five seconds under it. */
func resolveDialTimeoutDescription(clientConfig *ClientConfig) string {
    if 0 < clientConfig.DialTimeout {
        return clientConfig.DialTimeout.String()
    }

    return libraryDefaultDialTimeout.String() + " (library default)"
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

/* isNilInterface answers whether the interface value is nil outright or holds a nil pointer, map, slice, channel or function: a typed nil passes a plain nil comparison and then panics on first use, far from the wiring mistake that produced it. Duplicated from the framework's internal package, which a separate module cannot import. */
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
