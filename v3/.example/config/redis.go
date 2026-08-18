package config

import (
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/exception"
)

const (
    /* every key this application writes carries its major in the prefix. The three example applications share one redis in development, the cache holds gob-encoded entities whose wire format is keyed on the fully qualified type name — which carries the major — and the rate-limit counter is asserted on exactly by the end-to-end harness. A prefix shared between majors would have one application read entries it cannot decode and spend another's budget. */
    redisCacheKeyPrefix      = "melody-example-v3:cache:"
    redisRateLimitKeyPrefix  = "melody-example-v3:rate_limit:"
    redisTokenStoreKeyPrefix = "melody-example-v3:token"

    /* the allowance covers a person editing the nomenclature and stops a script: a burst of catalogue writes from one address is refused until the window rolls over. */
    catalogWriteAllowance = 30
)

func (instance *Module) buildRedis() {
    address := instance.environmentValue(environmentKeyRedisAddress)
    if "" == address {
        return
    }

    /* retry the initial connection with backoff so a cold-start race against the redis container does not
       hard-fail the boot, mirroring the database wiring. Only transient errors (connection refused) retry. */
    provider := melodyrueidis.NewProvider(
        melodyrueidis.WithRetryConfig(melodyrueidis.NewRetryConfig(10, time.Second, 5*time.Second, 2.0)),
    )

    client, openErr := provider.Open(melodyrueidis.NewConnectionParameters(address, "", ""))
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }

    instance.redisClient = client
    instance.serverSentEventBackplane = melodyrueidis.NewServerSentEventBackplane(client, instance.serverSentEventHub)
}
