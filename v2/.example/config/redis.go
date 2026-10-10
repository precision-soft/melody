package config

import (
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v2"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v2/cache"
    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyexception "github.com/precision-soft/melody/v2/exception"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    melodylogging "github.com/precision-soft/melody/v2/logging"
    "github.com/redis/rueidis"
)

const (
    ServiceExampleRedisCache      = "service.example.redis.cache"
    ServiceExampleRedisConnection = "service.example.redis.connection"

    /* every key carries the major in its prefix: the three example applications share one redis in development, and the gob-encoded entities key their wire format on the fully qualified type name, which carries the major, so a shared prefix would have one application read entries it cannot decode and spend another's rate-limit budget */
    redisCacheKeyPrefix     = "melody-example-v2:cache:"
    redisRateLimitKeyPrefix = "melody-example-v2:rate_limit:"

    /* the allowance covers a person editing the nomenclature and stops a script: a burst of catalogue writes from one address is refused until the window rolls over. */
    redisRateLimitAllowance = 30
    redisRateLimitWindow    = time.Minute
)

/* buildRedis opens the client while the modules are wired, because the rate-limit middleware is handed a live limiter when a route is declared. An unreachable endpoint is a warning rather than a boot failure, since the shipped .env names docker-compose services that do not resolve outside that network; the nomenclature then runs uncached and unthrottled. */
func (instance *Module) buildRedis(kernelInstance melodykernelcontract.Kernel) {
    address := parameterValue(kernelInstance, ParameterRedisAddress)
    if "" == address {
        return
    }

    provider := melodyrueidis.NewProvider(
        ParameterRedisAddress,
        ParameterRedisUser,
        ParameterRedisPassword,
    )

    client, openErr := provider.Open(newConfigurationResolver(kernelInstance))
    if nil != openErr {
        melodylogging.EmergencyLogger().Warning(
            "the example could not reach redis, so the nomenclature runs uncached and its writes are not throttled",
            melodyexception.LogContext(openErr),
        )

        return
    }

    cacheBackend, backendErr := melodyrueidiscache.NewBackendService(client, redisCacheKeyPrefix, 0, 0)
    if nil != backendErr {
        melodylogging.EmergencyLogger().Warning(
            "the example could not build the redis cache backend",
            melodyexception.LogContext(backendErr),
        )

        _ = provider.Close(client)

        return
    }

    instance.redisClient = client
    instance.redisCacheBackend = cacheBackend
    instance.redisRateLimiter = melodyrueidis.NewRateLimiter(
        client,
        redisRateLimitAllowance,
        redisRateLimitWindow,
        melodyrueidis.WithRateLimiterKeyPrefix(redisRateLimitKeyPrefix),
        melodyrueidis.WithRateLimiterFailureMode(melodyrueidis.FailureModeClosed),
    )
}

/* redisConnection closes the client the example opened. rueidis.Client.Close returns nothing, so it does not satisfy the closer the container looks for; this wrapper is what puts the connection into the container's ordered shutdown. */
type redisConnection struct {
    provider *melodyrueidis.Provider
    client   rueidis.Client
}

func (instance *redisConnection) Close() error {
    return instance.provider.Close(instance.client)
}

func (instance *Module) registerRedisServices(registrar melodyapplicationcontract.ServiceRegistrar) {
    if nil == instance.redisClient {
        return
    }

    client := instance.redisClient
    cacheBackend := instance.redisCacheBackend

    registrar.RegisterService(
        ServiceExampleRedisConnection,
        func(resolver melodycontainercontract.Resolver) (*redisConnection, error) {
            return &redisConnection{
                provider: melodyrueidis.NewProvider(
                    ParameterRedisAddress,
                    ParameterRedisUser,
                    ParameterRedisPassword,
                ),
                client: client,
            }, nil
        },
    )

    registrar.RegisterService(
        ServiceExampleRedisCache,
        func(resolver melodycontainercontract.Resolver) (*melodyrueidiscache.BackendService, error) {
            /* resolving the connection here is what makes the container close it: a service nothing resolves is never built, and one that is never built is never closed */
            _, connectionErr := resolver.Get(ServiceExampleRedisConnection)
            if nil != connectionErr {
                return nil, connectionErr
            }

            return cacheBackend, nil
        },
    )

    /* the same backend becomes the framework's cache, so the nomenclature survives a restart warm and the end-to-end harness can read the listing key out of band. The gob serializer keys its wire format on the type name, which carries the major, so the per-major prefix above is what keeps the three applications apart. */
    registrar.RegisterService(
        melodycache.ServiceCacheBackend,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
            return melodycontainer.FromResolver[*melodyrueidiscache.BackendService](resolver, ServiceExampleRedisCache)
        },
    )
}
