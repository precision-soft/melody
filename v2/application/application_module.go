package application

import (
    applicationcontract "github.com/precision-soft/melody/v2/application/contract"
    "github.com/precision-soft/melody/v2/exception"
    securityconfig "github.com/precision-soft/melody/v2/security/config"
)

func (instance *Application) RegisterModule(moduleInstance applicationcontract.Module) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register modules after boot", nil, nil))
    }

    if nil == moduleInstance {
        exception.Panic(
            exception.NewError("module instance may not be nil", nil, nil),
        )
    }

    instance.modules = append(instance.modules, moduleInstance)
}

func (instance *Application) bootModulesPreConfigurationResolve() {
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
