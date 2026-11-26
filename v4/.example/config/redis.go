package config

import (
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v4/cache"
    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    examplesecurity "github.com/precision-soft/melody/v4/.example/security"
    melodyapplicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    rueidis "github.com/redis/rueidis"
)

const (
    /* every key this application writes carries its major in the prefix. The three example applications share one redis in development, the cache holds gob-encoded entities whose wire format is keyed on the fully qualified type name — which carries the major — and the rate-limit counter is asserted on exactly by the end-to-end harness. A prefix shared between majors would have one application read entries it cannot decode and spend another's budget. */
    redisCacheKeyPrefixRoot  = "melody-example-v4:cache:"
    redisRateLimitKeyPrefix  = "melody-example-v4:rate_limit:"
    redisTokenStoreKeyPrefix = "melody-example-v4:token"

    /* the allowance covers a person editing the nomenclature and stops a script: a burst of catalogue writes from one address is refused until the window rolls over. */
    catalogWriteAllowance = 30

    /* the window the write allowance is counted over */
    catalogWriteWindow = time.Minute

    /* catalogWriteLimiterName keys the write budget's counters, shared by the write routes and the sign-in door */
    catalogWriteLimiterName = "catalog_write"

    /* the addresses the in-process budget tracks without redis; past it an unseen address takes the slot of the one seen longest ago rather than growing the map */
    inProcessWriteThrottleMaxAddresses = 10000
)

/* cacheKeyPrefix is the cache namespace with the layout token of the cached types inside it: the entities are gob-encoded under keys that lapse with their ttl, and gob decodes an older payload into a newer struct with the new field at zero, silently. A build reads only under the prefix of its own layout; the entries an older build left stand orphaned under their own token: example:db:reset clears the current layout's namespace alone, and an older one lapses with its ttl or is removed sooner by hand, as the readme says. */
func cacheKeyPrefix() string {
    return redisCacheKeyPrefixRoot + examplecache.LayoutToken() + ":"
}

/* buildRedis keeps what opening redis needs, and opens nothing: the connection is opened inside Boot, under the boot runtime, by the rueidis module's connection factory. */
func (instance *Module) buildRedis() {
    address := instance.parameterValue(parameterRedisAddress)
    if "" == address {
        return
    }

    /* retry the initial connection with backoff so a cold-start race against the redis container does not hard-fail the boot, mirroring the database wiring. Only transient errors (connection refused) retry, and the boot's context ends the wait. */
    instance.redisProvider = melodyrueidis.NewProvider(melodyrueidis.ProviderConfig{
        Retry: melodyrueidis.RetryConfig{
            MaxAttempts: bound.Of(10),
            Backoff:     bound.Backoff{Initial: bound.Of(time.Second), Max: bound.Of(5 * time.Second)},
        },
    })
    instance.redisParameters = melodyrueidis.ConnectionParameters{Address: address}
}

/* redisConfigured is the wiring fact every redis-backed choice reads: the address is set, so the connection service is registered */
func (instance *Module) redisConfigured() bool {
    return nil != instance.redisProvider
}

/* redisClientFromResolver answers the client of the connection service; resolving the connection records the edge that closes it after the caller's service */
func redisClientFromResolver(resolver melodycontainercontract.Resolver) (rueidis.Client, error) {
    connection, connectionErr := melodycontainer.FromResolver[*melodyrueidis.Connection](resolver.Runtime().Context(), resolver, melodyrueidis.ServiceConnection)
    if nil != connectionErr {
        return nil, connectionErr
    }

    return connection.Client(), nil
}

/* redisInfrastructure is the one switch for a process that has redis: the connection, the token store and the cache backend live on the same client, so they are registered together or not at all, rather than as registrations that must stay paired by hand. */
type redisInfrastructure struct {
    provider           *melodyrueidis.Provider
    parameters         melodyrueidis.ConnectionParameters
    serverSentEventHub *melodyhttp.ServerSentEventHub
}

func newRedisInfrastructure(
    provider *melodyrueidis.Provider,
    parameters melodyrueidis.ConnectionParameters,
    serverSentEventHub *melodyhttp.ServerSentEventHub,
) *redisInfrastructure {
    return &redisInfrastructure{
        provider:           provider,
        parameters:         parameters,
        serverSentEventHub: serverSentEventHub,
    }
}

/* openConnection opens redis under the boot runtime and installs the backplane on the hub, the one holder: the hub's Shutdown drains the publishes in flight and closes the backplane's listen goroutine and subscription, and the hub's registration is closed before this connection. */
func (instance *redisInfrastructure) openConnection(resolver melodycontainercontract.Resolver) (*melodyrueidis.Connection, error) {
    logger, loggerErr := melodylogging.LoggerFromResolver(resolver.Runtime().Context(), resolver)
    if nil != loggerErr {
        return nil, loggerErr
    }

    connection, openErr := instance.provider.Open(resolver.Runtime(), instance.parameters)
    if nil != openErr {
        return nil, openErr
    }

    melodyrueidis.NewServerSentEventBackplane(connection.Client(), instance.serverSentEventHub, melodyrueidis.ServerSentEventBackplaneConfig{
        Logger: logger,
    })

    return connection, nil
}

func (instance *redisInfrastructure) Modules() []melodyapplicationcontract.Module {
    return []melodyapplicationcontract.Module{
        melodyrueidis.NewModule(melodyrueidis.ModuleConfig{
            Connection: instance.openConnection,
            /* redis fails the boot as the database does: a process whose store is unreachable is not started */
            ConnectionBuiltAtBoot: true, ConnectionBootFailure: melodycontainer.BootFailureFailsBoot,
            Locker:     &melodyrueidis.LockerConfig{},
            TokenStore: &melodyrueidis.TokenStoreConfig{Prefix: redisTokenStoreKeyPrefix},
        }),
        melodyrueidiscache.NewModule(melodyrueidiscache.ModuleConfig{
            Backend: melodyrueidiscache.BackendConfig{
                Prefix: cacheKeyPrefix(),
                /* the default, written out: this budget is what keeps a store that stops answering from holding a request-path read */
                CallTimeout: bound.Of(time.Second),
            },
        }),
    }
}

var _ melodyapplicationcontract.ModuleProvider = (*redisInfrastructure)(nil)

/* runtimeResolvedRateLimiter is a redis limiter as a middleware built at wiring holds it: the limiter is a service over the connection opened at boot, resolved through each call's runtime. */
type runtimeResolvedRateLimiter struct {
    serviceName string
}

func (instance *runtimeResolvedRateLimiter) Allow(runtimeInstance melodycontainercontract.Runtime, key string) (melodyhttpcontract.RateLimitDecision, error) {
    limiter, resolveErr := melodyruntime.FromRuntime[melodyhttpcontract.RateLimiter](runtimeInstance, instance.serviceName)
    if nil != resolveErr {
        return melodyhttpcontract.RateLimitDecision{}, resolveErr
    }

    return limiter.Allow(runtimeInstance, key)
}

func (instance *runtimeResolvedRateLimiter) Reset(runtimeInstance melodycontainercontract.Runtime, key string) error {
    limiter, resolveErr := melodyruntime.FromRuntime[melodyhttpcontract.RateLimiter](runtimeInstance, instance.serviceName)
    if nil != resolveErr {
        return resolveErr
    }

    return limiter.Reset(runtimeInstance, key)
}

var _ melodyhttpcontract.RateLimiter = (*runtimeResolvedRateLimiter)(nil)

/* runtimeResolvedNonceGuard is a redis nonce guard as a firewall built at wiring holds it, resolved through each call's runtime */
type runtimeResolvedNonceGuard struct {
    serviceName string
}

func (instance *runtimeResolvedNonceGuard) Remember(runtimeInstance melodycontainercontract.Runtime, nonce string, ttl bound.Duration) (bool, error) {
    guard, resolveErr := melodyruntime.FromRuntime[melodysecuritycontract.NonceGuard](runtimeInstance, instance.serviceName)
    if nil != resolveErr {
        return false, resolveErr
    }

    return guard.Remember(runtimeInstance, nonce, ttl)
}

var _ melodysecuritycontract.NonceGuard = (*runtimeResolvedNonceGuard)(nil)

const (
    serviceCatalogWriteLimiter        = "service.example.redis.catalog_write_limiter"
    serviceSecondFactorBudgetLimiter  = "service.example.redis.second_factor_budget_limiter"
    serviceSecondFactorReplayGuard    = "service.example.redis.second_factor_replay_guard"
    serviceInternalNonceGuard         = "service.example.redis.internal_nonce_guard"
)

/* registerRedisServices registers the redis-backed collaborators over the connection service, each built at its first resolve, after the boot opened the connection */
func (instance *Module) registerRedisServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    if false == instance.redisConfigured() {
        return
    }

    registrar.RegisterService(
        examplesecurity.ServiceTokenNamespace,
        func(resolver melodycontainercontract.Resolver) (*examplesecurity.TokenNamespace, error) {
            client, clientErr := redisClientFromResolver(resolver)
            if nil != clientErr {
                return nil, clientErr
            }

            return examplesecurity.NewTokenNamespace(client, redisTokenStoreKeyPrefix), nil
        },
        melodycontainer.RegisterOptions{},
    )

    for _, limiter := range []struct {
        serviceName string
        name        string
        allowance   int
        window      time.Duration
    }{
        {serviceCatalogWriteLimiter, catalogWriteLimiterName, catalogWriteAllowance, catalogWriteWindow},
        {serviceSecondFactorBudgetLimiter, secondFactorBudgetLimiterName, secondFactorBudgetAllowance, secondFactorBudgetWindow},
    } {
        registrar.RegisterService(
            limiter.serviceName,
            func(resolver melodycontainercontract.Resolver) (melodyhttpcontract.RateLimiter, error) {
                client, clientErr := redisClientFromResolver(resolver)
                if nil != clientErr {
                    return nil, clientErr
                }

                return melodyrueidis.NewRateLimiter(client, limiter.name, limiter.allowance, limiter.window, melodyrueidis.RateLimiterConfig{
                    KeyPrefix: redisRateLimitKeyPrefix,
                }), nil
            },
            melodycontainer.RegisterOptions{TypeRegistrationDisabled: true},
        )
    }

    for _, guard := range []struct {
        serviceName string
        keyPrefix   string
    }{
        {serviceSecondFactorReplayGuard, secondFactorReplayGuardPrefix},
        {serviceInternalNonceGuard, internalNonceGuardPrefix},
    } {
        registrar.RegisterService(
            guard.serviceName,
            func(resolver melodycontainercontract.Resolver) (melodysecuritycontract.NonceGuard, error) {
                client, clientErr := redisClientFromResolver(resolver)
                if nil != clientErr {
                    return nil, clientErr
                }

                return melodyrueidis.NewNonceGuard(client, melodyrueidis.NonceGuardConfig{KeyPrefix: guard.keyPrefix}), nil
            },
            melodycontainer.RegisterOptions{TypeRegistrationDisabled: true},
        )
    }
}
