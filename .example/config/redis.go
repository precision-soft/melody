package config

import (
    "time"

    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
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

    /* the demo keys carry a prefix of their own. The three example applications share one redis in development, and the rate-limit counter in particular is asserted on exactly by the end-to-end harness, so a prefix shared between majors would have one application spend another's budget. */
    redisCacheKeyPrefix     = "melody-example-app:demo:"
    redisRateLimitKeyPrefix = "melody-example-app:rate_limit:"

    redisRateLimitAllowance = 5
    redisRateLimitWindow    = time.Minute
)

/* buildRedis opens the client while the modules are wired, because the rate-limit middleware is handed a live limiter at the moment a route is declared and refuses a nil one.

An unreachable endpoint is a warning rather than a boot failure. The shipped .env points at the docker-compose service names, which do not resolve outside that network, so panicking here would make `go run .` on a laptop — and every command-line invocation in a machine without the stack — die at boot over a demo. The routes are simply not registered, and the end-to-end harness turns the soft failure back into a hard one where it knows redis is up. */
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
            "the example could not reach redis, so its cache and rate-limit demos are not registered",
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

    /* the backend is published under the example's own name rather than as the framework's cache backend. The application caches domain entities through a gob serializer, and gob keys its wire format on the fully qualified type name — which carries the major — so pointing the shared cache at one redis would have the three example applications write entries none of the others can decode. The demo below stores raw bytes, which cross majors safely. */
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
}
