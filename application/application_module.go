package application

import (
    applicationcontract "github.com/precision-soft/melody/application/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    securityconfig "github.com/precision-soft/melody/security/config"
)

const maxModuleProviderDepth = 100

func (instance *Application) RegisterModule(moduleInstance applicationcontract.Module) {
    instance.registerModuleAtDepth(moduleInstance, 0)
}

func (instance *Application) registerModuleAtDepth(moduleInstance applicationcontract.Module, depth int) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register modules after boot", nil, nil))
    }

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

    /* read through the interface, like the module door above: a typed nil reaches the Modules() call on the next line */
    if true == internal.IsNilInterface(provider) {
        exception.Panic(
            exception.NewError("module provider may not be nil", nil, nil),
        )
    }

    for _, providedModule := range provider.Modules() {
        instance.RegisterModule(providedModule)
    }
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
        if securityModule, ok := moduleInstance.(SecurityModule); true == ok {
            securityModule.RegisterSecurity(securityBuilder)
        }
    }
    compiledConfiguration := securityBuilder.BuildAndCompile()
    if nil != compiledConfiguration {
        instance.securityConfiguration = compiledConfiguration
    }

    for _, moduleInstance := range instance.modules {
        if eventsModule, ok := moduleInstance.(applicationcontract.EventModule); true == ok {
            eventsModule.RegisterEventSubscribers(instance.kernel)
        }

        if httpMiddlewareModule, ok := moduleInstance.(applicationcontract.HttpMiddlewareModule); true == ok {
            httpMiddlewareModule.RegisterHttpMiddlewares(instance.kernel, instance.httpMiddlewares)
        }

        if httpModule, ok := moduleInstance.(applicationcontract.HttpModule); true == ok {
            httpModule.RegisterHttpRoutes(instance.kernel)
        }

        if cliModule, ok := moduleInstance.(applicationcontract.CliModule); true == ok {
            commands := cliModule.RegisterCliCommands(instance.kernel)

            for _, command := range commands {
                instance.RegisterCliCommand(command)
            }
        }
    }
}
