package config

import (
    "time"

    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodycache "github.com/precision-soft/melody/cache"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyexception "github.com/precision-soft/melody/exception"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/cache"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
    melodylogging "github.com/precision-soft/melody/logging"
    "github.com/redis/rueidis"
)

const (
    ServiceExampleRedisCache      = "service.example.redis.cache"
    ServiceExampleRedisConnection = "service.example.redis.connection"

    /* every key this application writes carries its major in the prefix. The three example applications share one redis in development, the cache holds gob-encoded entities whose wire format is keyed on the fully qualified type name — which carries the major — and the rate-limit counter is asserted on exactly by the end-to-end harness. A prefix shared between majors would have one application read entries it cannot decode and spend another's budget. */
    redisCacheKeyPrefix     = "melody-example-v1:cache:"
    redisRateLimitKeyPrefix = "melody-example-v1:rate_limit:"

    /* the allowance covers a person editing the nomenclature and stops a script: a burst of catalogue writes from one address is refused until the window rolls over. */
    redisRateLimitAllowance = 30
    redisRateLimitWindow    = time.Minute
)

/* buildRedis opens the client while the modules are wired, because the rate-limit middleware is handed a live limiter at the moment a route is declared and refuses a nil one.

An unreachable endpoint is a warning rather than a boot failure. The shipped .env points at the docker-compose service names, which do not resolve outside that network, so panicking here would make `go run .` on a laptop — and every command-line invocation on a machine without the stack — die at boot. The nomenclature simply runs uncached and unthrottled, and the end-to-end harness turns the soft failure back into a hard one where it knows redis is up. */
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

    /* the same backend also becomes the framework's, so the nomenclature itself is cached in redis rather than only inside this process — which is what lets the catalogue survive a restart warm, and what lets the end-to-end harness read the listing key out of band instead of taking the application's word for it.

       The application caches domain entities through a gob serializer, and gob keys its wire format on the fully qualified type name, which carries the major. Three example applications sharing one redis would therefore write entries none of the others can decode — the per-major key prefix above is what keeps them apart, and it is the reason this registration is safe. */
    registrar.RegisterService(
        melodycache.ServiceCacheBackend,
        func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
            return melodycontainer.FromResolver[*melodyrueidiscache.BackendService](resolver, ServiceExampleRedisCache)
        },
    )
}
