package application

import (
    "reflect"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    securityconfig "github.com/precision-soft/melody/v3/security/config"
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

    if false == reflect.ValueOf(moduleInstance).Comparable() {
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

    if true == internal.IsNilInterface(provider) {
        exception.Panic(
            exception.NewError("module provider may not be nil", nil, nil),
        )
    }

    if moduleInstance, isModule := provider.(applicationcontract.Module); true == isModule {
        instance.registerModuleAtDepth(moduleInstance, 0)

        return
    }

    for _, providedModule := range provider.Modules() {
        instance.registerModuleAtDepth(providedModule, 1)
    }
}

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
            serviceModule.RegisterServices(instance)
        }
    }

    for _, moduleInstance := range instance.modules {
        if scopedServiceModule, ok := moduleInstance.(applicationcontract.ScopedServiceModule); true == ok {
            scopedServiceModule.RegisterScopedServices(instance)
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
    }

    for _, moduleInstance := range instance.modules {
        if httpMiddlewareModule, ok := moduleInstance.(applicationcontract.HttpMiddlewareModule); true == ok {
            httpMiddlewareModule.RegisterHttpMiddlewares(instance.kernel, instance.httpMiddlewares)
        }
    }

    for _, moduleInstance := range instance.modules {
        if decoratorModule, ok := moduleInstance.(applicationcontract.HttpHandlerDecoratorModule); true == ok {
            for _, decorator := range decoratorModule.RegisterHttpHandlerDecorators(instance.kernel) {
                if nil == decorator {
                    continue
                }

                instance.httpHandlerDecorators = append(instance.httpHandlerDecorators, decorator)
            }
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
