package application

import (
	applicationcontract "github.com/precision-soft/melody/application/contract"
	"github.com/precision-soft/melody/exception"
	securityconfig "github.com/precision-soft/melody/security/config"
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

func (instance *Application) bootModules() {
	securityBuilder := securityconfig.NewBuilder()

	for _, moduleInstance := range instance.modules {
		if securityModule, ok := moduleInstance.(SecurityModule); true == ok {
			securityModule.RegisterSecurity(securityBuilder)
		}

		if eventsModule, ok := moduleInstance.(applicationcontract.EventModule); true == ok {
			eventsModule.RegisterEventSubscribers(instance.kernel)
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

	compiledConfiguration := securityBuilder.BuildAndCompile()
	if nil != compiledConfiguration {
		instance.securityConfiguration = compiledConfiguration
	}
}
