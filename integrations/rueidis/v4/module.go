package rueidis

import (
    "reflect"

    "github.com/redis/rueidis"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* ModuleConfig declares what the module registers: the connection service its factory builds, and the locker and token store services built over it, each set by a pointer to its configuration. */
type ModuleConfig struct {
    /* Connection is the provider of ServiceConnection, typically provider.Open(resolver.Runtime(), parameters); nil registers nothing. Every service this module registers resolves the connection, so the container's ordered teardown closes it after them. */
    Connection func(resolver containercontract.Resolver) (*Connection, error)

    /* ConnectionBuiltAtBoot builds the connection inside Boot, under the boot runtime, so an unreachable store is known before the first request. */
    ConnectionBuiltAtBoot bool

    /* ConnectionBootFailure is the boot failure mode of a connection built at boot, the registrar's own: BootFailureWarns when left out. */
    ConnectionBootFailure containercontract.BootFailureMode

    /* Locker registers the locker service built from it; nil registers none, &LockerConfig{} the default locker. */
    Locker *LockerConfig

    /* TokenStore registers the revocable token store built from it; nil registers none, &TokenStoreConfig{} the default store. */
    TokenStore *TokenStoreConfig
}

/* NewModule copies the role configurations it is handed, so a later edit of the caller's value is not observed. */
func NewModule(config ModuleConfig) *Module {
    if nil != config.Locker {
        locker := *config.Locker
        config.Locker = &locker
    }

    if nil != config.TokenStore {
        tokenStore := *config.TokenStore
        config.TokenStore = &tokenStore
    }

    return &Module{config: config}
}

type Module struct {
    config ModuleConfig
}

func (instance *Module) Name() string {
    return "rueidis"
}

func (instance *Module) Description() string {
    return "registers the redis connection and optionally the locker and the revocable token store services over it"
}

/* clientIsNil reads a typed nil as nil, which a bare comparison on the interface takes for a live client. */
func clientIsNil(client rueidis.Client) bool {
    if nil == client {
        return true
    }

    value := reflect.ValueOf(client)

    return reflect.Pointer == value.Kind() && true == value.IsNil()
}

/* refuseWhatTheModuleCannotServe fails the boot on a role or a boot mode handed to a module with no connection factory, and on a boot failure mode for a connection not built at boot, where nothing would be registered and the service would be missed only at its first resolve. */
func refuseWhatTheModuleCannotServe(config ModuleConfig) {
    hasConnection := nil != config.Connection

    for _, role := range []struct {
        field string
        set   bool
    }{
        {"ModuleConfig.Locker", nil != config.Locker},
        {"ModuleConfig.TokenStore", nil != config.TokenStore},
    } {
        if true == role.set && false == hasConnection {
            exception.Panic(exception.NewError(
                "the rueidis module is asked for a role and has no connection",
                map[string]any{"field": role.field, "requires": "ModuleConfig.Connection"},
                nil,
            ))
        }
    }

    if true == config.ConnectionBuiltAtBoot && false == hasConnection {
        exception.Panic(exception.NewError(
            "the rueidis module is handed a boot mode and has no connection built at boot",
            map[string]any{"field": "ModuleConfig.ConnectionBuiltAtBoot", "requires": "ModuleConfig.Connection"},
            nil,
        ))
    }

    if containercontract.BootFailureWarns != config.ConnectionBootFailure && (false == hasConnection || false == config.ConnectionBuiltAtBoot) {
        requires := "ModuleConfig.ConnectionBuiltAtBoot"
        if false == hasConnection {
            requires = "ModuleConfig.Connection"
        }

        exception.Panic(exception.NewError(
            "the rueidis module is handed a boot mode and has no connection built at boot",
            map[string]any{"field": "ModuleConfig.ConnectionBootFailure", "requires": requires},
            nil,
        ))
    }
}

func (instance *Module) RegisterServices(registrar applicationcontract.ServiceRegistrar) {
    refuseWhatTheModuleCannotServe(instance.config)

    if nil == instance.config.Connection {
        return
    }

    registerConnectionService(
        registrar,
        instance.config.Connection,
        containercontract.RegisterOptions{
            BuiltAtBoot: instance.config.ConnectionBuiltAtBoot,
            BootFailure: instance.config.ConnectionBootFailure,
        },
    )

    if nil != instance.config.Locker {
        registerLockerService(registrar, *instance.config.Locker)
    }

    if nil != instance.config.TokenStore {
        registerTokenStoreService(registrar, *instance.config.TokenStore)
    }
}

var (
    _ applicationcontract.Module        = (*Module)(nil)
    _ applicationcontract.ServiceModule = (*Module)(nil)
)
