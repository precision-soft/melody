package config

import (
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v3/cache"
    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/exception"
    rueidis "github.com/redis/rueidis"
)

const (
    /* every key this application writes carries its major in the prefix. The three example applications share one redis in development, the cache holds gob-encoded entities whose wire format is keyed on the fully qualified type name — which carries the major — and the rate-limit counter is asserted on exactly by the end-to-end harness. A prefix shared between majors would have one application read entries it cannot decode and spend another's budget. */
    redisCacheKeyPrefixRoot  = "melody-example-v3:cache:"
    redisRateLimitKeyPrefix  = "melody-example-v3:rate_limit:"
    redisTokenStoreKeyPrefix = "melody-example-v3:token"

    /* the allowance covers a person editing the nomenclature and stops a script: a burst of catalogue writes from one address is refused until the window rolls over. */
    catalogWriteAllowance = 30

    /* the window the write allowance is counted over */
    catalogWriteWindow = time.Minute

    /* the addresses the in-process budget tracks without redis; past it an unseen address is refused rather than growing the map */
    inProcessWriteThrottleMaxAddresses = 10000
)

/* cacheKeyPrefix is the cache namespace with the layout token of the cached types inside it: the entities are gob-encoded under keys with no expiry, and gob decodes an older payload into a newer struct with the new field at zero, silently. A build reads only under the prefix of its own layout; the entries an older build left stand orphaned until example:db:reset clears the namespace, which the readme says. */
func cacheKeyPrefix() string {
    return redisCacheKeyPrefixRoot + examplecache.LayoutToken() + ":"
}

func (instance *Module) buildRedis() {
    address := instance.environmentValue(environmentKeyRedisAddress)
    if "" == address {
        return
    }

    /* retry the initial connection with backoff so a cold-start race against the redis container does not hard-fail the boot, mirroring the database wiring. Only transient errors (connection refused) retry. */
    provider := melodyrueidis.NewProvider(
        melodyrueidis.WithRetryConfig(melodyrueidis.NewRetryConfig(10, time.Second, 5*time.Second, 2.0)),
    )

    client, openErr := provider.Open(melodyrueidis.NewConnectionParameters(address, "", ""))
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }

    instance.redisClient = client

    /* the raw client's Close returns nothing, so on its own it can never join the container's ordered teardown; the Connection wrapper is registered through the rueidis module as the service that owns it, and the first resolved client-backed service (the token store, the cache backend, the locker) records the edge that closes the connection after it */
    instance.redisConnection = melodyrueidis.NewConnection(client)
    /* the constructor installs the backplane on the hub and that installation is its whole effect here: the hub is the only holder, and its Shutdown is what drains the publishes in flight and closes the backplane's listen goroutine and subscription. The composition root keeps no reference of its own, because a second holder is a second closer. */
    melodyrueidis.NewServerSentEventBackplane(client, instance.serverSentEventHub)
}

/* redisInfrastructure is the one switch for a process that has redis: the token store and the cache backend live on the same client, so they are registered together or not at all, rather than as two registrations that must stay paired by hand. */
type redisInfrastructure struct {
    client     rueidis.Client
    connection *melodyrueidis.Connection
}

func newRedisInfrastructure(client rueidis.Client, connection *melodyrueidis.Connection) *redisInfrastructure {
    return &redisInfrastructure{
        client:     client,
        connection: connection,
    }
}

func (instance *redisInfrastructure) Modules() []melodyapplicationcontract.Module {
    return []melodyapplicationcontract.Module{
        melodyrueidis.NewModule(melodyrueidis.ModuleConfig{
            Client:       instance.client,
            Connection:   instance.connection,
            AsTokenStore: true,
            TokenStoreOptions: []melodyrueidis.TokenStoreOption{
                melodyrueidis.WithTokenStorePrefix(redisTokenStoreKeyPrefix),
            },
        }),
        melodyrueidiscache.NewModule(melodyrueidiscache.ModuleConfig{
            Client: instance.client,
            Prefix: cacheKeyPrefix(),
            /* the backend's context-less doors run unbounded without this; a store that stops answering would hold a request-path read for good */
            BackendOptions: []melodyrueidiscache.BackendOption{
                melodyrueidiscache.WithCommandTimeout(time.Second),
            },
        }),
    }
}

var _ melodyapplicationcontract.ModuleProvider = (*redisInfrastructure)(nil)
