package config

import (
    "os"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/exception"
)

func (instance *Module) buildRedis() {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        return
    }

    provider := melodyrueidis.NewProvider()

    client, openErr := provider.Open(melodyrueidis.NewConnectionParams(address, "", ""))
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }

    instance.redisClient = client
    instance.serverSentEventBackplane = melodyrueidis.NewServerSentEventBackplane(client, instance.serverSentEventHub)
}
