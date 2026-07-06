package config

import (
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/exception"
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

    client, openErr := provider.Open(melodyrueidis.NewConnectionParams(address, "", ""))
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }

    instance.redisClient = client
    instance.serverSentEventBackplane = melodyrueidis.NewServerSentEventBackplane(client, instance.serverSentEventHub)
}
