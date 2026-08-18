package application

import (
    "reflect"

    applicationcontract "github.com/precision-soft/melody/v2/application/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    securityconfig "github.com/precision-soft/melody/v2/security/config"
)

const maxModuleProviderDepth = 100

func (instance *Application) RegisterModule(moduleInstance applicationcontract.Module) {
    instance.registerModuleAtDepth(moduleInstance, 0)
}

func (instance *Application) registerModuleAtDepth(moduleInstance applicationcontract.Module, depth int) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register modules after boot", nil, nil))
    }

    instance.refuseModuleRegistrationDuringBoot()

    /* read through the interface: a typed nil passes a plain comparison, is stored, and boots reporting success — the failure then surfaces as a bare dereference inside the module's own hook rather than as the refusal this guard exists to give */
    if true == internal.IsNilInterface(moduleInstance) {
        exception.Panic(
            exception.NewError("module instance may not be nil", nil, nil),
        )
    }

    if depth > maxModuleProviderDepth {
        exception.Panic(
            exception.NewError("module provider expansion exceeded maximum depth, possible provider cycle", nil, nil),
        )
    }

    /* a module's identity is its instance: the same instance reached through two providers used to boot twice, and the loud half of that — a duplicate service name — hid the silent half, its listeners and middlewares each attached twice. The skip covers the children too: they were expanded when the instance first registered. An instance of an uncomparable type cannot be a map key, so it keeps the old behavior rather than a runtime panic; Name stays informative and two distinct instances sharing one name stay two modules. */
    if false == reflect.TypeOf(moduleInstance).Comparable() {
        instance.appendModule(moduleInstance, depth)

        return
    }

    if nil == instance.registeredModuleInstances {
        instance.registeredModuleInstances = make(map[applicationcontract.Module]struct{})
    }

    if _, exists := instance.registeredModuleInstances[moduleInstance]; true == exists {
        return
    }

    instance.registeredModuleInstances[moduleInstance] = struct{}{}

    instance.appendModule(moduleInstance, depth)
}

func (instance *Application) appendModule(moduleInstance applicationcontract.Module, depth int) {
    instance.modules = append(instance.modules, moduleInstance)

    if moduleProvider, ok := moduleInstance.(applicationcontract.ModuleProvider); true == ok {
        for _, providedModule := range moduleProvider.Modules() {
            instance.registerModuleAtDepth(providedModule, depth+1)
        }
    }
}

func (instance *Application) RegisterModuleProvider(provider applicationcontract.ModuleProvider) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register modules after boot", nil, nil))
    }

    instance.refuseModuleRegistrationDuringBoot()

    /* read through the interface, like the module door above: a typed nil reaches the type assertion on the next line */
    if true == internal.IsNilInterface(provider) {
        exception.Panic(
            exception.NewError("module provider may not be nil", nil, nil),
        )
    }

    /* a provider that is itself a module is delegated whole, so its own hooks boot exactly as they do through RegisterModule — this door used to keep only the children and silently drop the provider's own registrations, which made the two doors register different applications from the same value */
    if moduleInstance, isModule := provider.(applicationcontract.Module); true == isModule {
        instance.registerModuleAtDepth(moduleInstance, 0)

        return
    }

    for _, providedModule := range provider.Modules() {
        instance.registerModuleAtDepth(providedModule, 1)
    }
}

/* refuseModuleRegistrationDuringBoot closes both module doors for the boot window: the phase loops iterate a snapshot of the module list, so a module registered from inside a boot hook would receive only the hooks of the phases still ahead — booted by whatever fraction of the lifecycle had not run yet, reported as a success. */
func (instance *Application) refuseModuleRegistrationDuringBoot() {
    if false == instance.booting {
        return
    }

    exception.Panic(
        exception.NewError(
            "may not register a module from inside a module boot hook; a module that carries other modules implements ModuleProvider",
            nil,
            nil,
        ),
    )
}

func (instance *Application) bootModulesPreConfigurationResolve() {
    for _, moduleInstance := range instance.modules {
        if configModule, ok := moduleInstance.(applicationcontract.ConfigModule); true == ok {
            configModule.RegisterConfigurations(instance)
        }
    }

    for _, moduleInstance := range instance.modules {
        if parameterModule, ok := moduleInstance.(applicationcontract.ParameterModule); true == ok {
            parameterModule.RegisterParameters(instance)
        }
    }
}

func (instance *Application) bootModulesPostConfigurationResolve() {
    for _, moduleInstance := range instance.modules {
        if serviceModule, ok := moduleInstance.(applicationcontract.ServiceModule); true == ok {
            serviceModule.RegisterServices(instance.kernel, instance)
        }
    }

    /* the scoped registrations come after the process-lifetime ones and before the framework's own, so a module scoping a name the framework registers later meets the refusal from either side and lands in the aggregated boot report beside its siblings */
    for _, moduleInstance := range instance.modules {
        if scopedServiceModule, ok := moduleInstance.(applicationcontract.ScopedServiceModule); true == ok {
            scopedServiceModule.RegisterScopedServices(instance.kernel, instance)
        }
    }

    securityBuilder := securityconfig.NewBuilder()
    for _, moduleInstance := range instance.modules {
        if securityModule, ok := moduleInstance.(applicationcontract.SecurityModule); true == ok {
            securityModule.RegisterSecurity(securityBuilder)
        }
    }
    compiledConfiguration := securityBuilder.BuildAndCompile()
    if nil != compiledConfiguration {
        instance.securityConfiguration = compiledConfiguration
    }

    /* one loop per hook, like every phase above: each hook runs across every module before the next hook begins, which is the granularity the contracts document — a module may rely on every sibling's listeners existing before any middleware registers, and folding hooks into one sweep would silently change that rule for exactly these four */
    for _, moduleInstance := range instance.modules {
        if eventsModule, ok := moduleInstance.(applicationcontract.EventModule); true == ok {
            eventsModule.RegisterEventSubscribers(instance.kernel)
        }
    }

    for _, moduleInstance := range instance.modules {
        if httpMiddlewareModule, ok := moduleInstance.(applicationcontract.HttpMiddlewareModule); true == ok {
            httpMiddlewareModule.RegisterHttpMiddlewares(instance.kernel, instance.httpMiddlewares)
        }
    }

    for _, moduleInstance := range instance.modules {
        if httpModule, ok := moduleInstance.(applicationcontract.HttpModule); true == ok {
            httpModule.RegisterHttpRoutes(instance.kernel)
        }
    }

    for _, moduleInstance := range instance.modules {
        if cliModule, ok := moduleInstance.(applicationcontract.CliModule); true == ok {
            commands := cliModule.RegisterCliCommands(instance.kernel)

            for _, command := range commands {
                instance.RegisterCliCommand(command)
            }
        }
    }
}
