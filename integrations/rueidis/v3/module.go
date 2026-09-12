package rueidis

import (
    "github.com/redis/rueidis"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
)

type ModuleConfig struct {
    Client            rueidis.Client
    AsLocker          bool
    AsTokenStore      bool
    TokenStoreOptions []TokenStoreOption

    /* LockerOptions are handed to the locker registered under AsLocker — WithLockerCallTimeout above all —, the way TokenStoreOptions reach the token store. */
    LockerOptions []LockerOption

    /* Connection, when set, is registered as the service that OWNS the client, so the container's ordered teardown finally closes it — the raw client's Close returns nothing, so registered alone it can never join the teardown, and it used to live exactly as long as the process. Wrap the opened client with NewConnection and hand both in (or just the Connection: a nil Client is then read off it). Every client-backed service this module registers resolves the connection as its dependency, so whichever run resolves one of them orders the connection's close after it; a run that resolves none leaves the connection unclosed, as the messagebus transports document for the same shape. */
    Connection *Connection
}

func NewModule(config ModuleConfig) *Module {
    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "rueidis"
}

func (instance *Module) Description() string {
    return "registers the redis client and optionally the connection owner, the locker and the revocable token store services"
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    client := instance.config.Client
    if nil == client && nil != instance.config.Connection {
        client = instance.config.Connection.Client()
    }

    if nil == client {
        return
    }

    if nil != instance.config.Connection {
        RegisterConnectionService(registrar, instance.config.Connection)
    }

    RegisterClientService(registrar, client)

    if true == instance.config.AsLocker {
        RegisterLockerServiceWithOptions(registrar, client, instance.config.LockerOptions...)
    }

    if true == instance.config.AsTokenStore {
        RegisterTokenStoreService(registrar, client, instance.config.TokenStoreOptions...)
    }
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)
