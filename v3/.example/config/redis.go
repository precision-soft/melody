package config

import (
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    examplecache "github.com/precision-soft/melody/v3/.example/cache"
    "github.com/precision-soft/melody/v3/exception"
)

const (

    redisCacheKeyPrefixRoot  = "melody-example-v3:cache:"
    redisRateLimitKeyPrefix  = "melody-example-v3:rate_limit:"
    redisTokenStoreKeyPrefix = "melody-example-v3:token"

    catalogWriteAllowance = 30
)

func cacheKeyPrefix() string {
    return redisCacheKeyPrefixRoot + examplecache.LayoutToken() + ":"
}

func (instance *Module) buildRedis() {
    address := instance.environmentValue(environmentKeyRedisAddress)
    if "" == address {
        return
    }

    provider := melodyrueidis.NewProvider(
        melodyrueidis.WithRetryConfig(melodyrueidis.NewRetryConfig(10, time.Second, 5*time.Second, 2.0)),
    )

    client, openErr := provider.Open(melodyrueidis.NewConnectionParameters(address, "", ""))
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }

    instance.redisClient = client

    instance.redisConnection = melodyrueidis.NewConnection(client)

    melodyrueidis.NewServerSentEventBackplane(client, instance.serverSentEventHub)
}
