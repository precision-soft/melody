package application

import (
    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* populated lazily, since tests assemble a bare Application without the constructor */
func (instance *Application) bootWarningRegistryOf() *internal.BootWarningRegistry {
    if nil == instance.bootWarningRegistry {
        instance.bootWarningRegistry = internal.NewBootWarningRegistry()
        instance.bootWarningOwners = make(map[string]string)
    }

    return instance.bootWarningRegistry
}

func (instance *Application) bootModuleBootWarnings() {
    registry := instance.bootWarningRegistryOf()

    for _, moduleInstance := range instance.modules {
        if bootWarningModule, ok := moduleInstance.(applicationcontract.BootWarningModule); true == ok {
            bootWarningModule.RegisterBootWarnings(&moduleBootWarningRegistrar{
                application: instance,
                registry:    registry,
                moduleName:  moduleInstance.Name(),
            })
        }
    }
}

type moduleBootWarningRegistrar struct {
    application *Application
    registry    *internal.BootWarningRegistry
    moduleName  string
}

func (instance *moduleBootWarningRegistrar) DeclareBootWarning(name string, writtenInCli bool) {
    if "" == name {
        exception.Panic(exception.NewError(
            "a module declared a boot warning with an empty name",
            exceptioncontract.Context{"module": instance.moduleName},
            nil,
        ))
    }

    owner, declaredByModule := instance.application.bootWarningOwners[name]

    /* a module registered twice, one instance per configuration, declares its code twice */
    if true == declaredByModule && owner == instance.moduleName && writtenInCli == instance.registry.WrittenInCli(name) {
        return
    }

    if false == instance.registry.Declare(name, writtenInCli) {
        if false == declaredByModule {
            owner = "the framework"
        }

        exception.Panic(exception.NewError(
            "a module declared a boot warning name another owner already holds",
            exceptioncontract.Context{"module": instance.moduleName, "name": name, "owner": owner},
            nil,
        ))
    }

    instance.application.bootWarningOwners[name] = instance.moduleName
}

func (instance *moduleBootWarningRegistrar) AddBootWarning(warning applicationcontract.BootWarning) {
    if owner, declaredByModule := instance.application.bootWarningOwners[warning.Name]; false == declaredByModule || owner != instance.moduleName {
        exception.Panic(exception.NewError(
            "a module added a boot warning under a name it did not declare",
            exceptioncontract.Context{"module": instance.moduleName, "name": warning.Name},
            nil,
        ))
    }

    if "" == warning.Message {
        exception.Panic(exception.NewError(
            "a module added a boot warning with an empty message",
            exceptioncontract.Context{"module": instance.moduleName, "name": warning.Name},
            nil,
        ))
    }

    instance.application.bootWarnings = append(instance.application.bootWarnings, internal.BootWarning{
        Name:    warning.Name,
        Message: warning.Message,
        Context: warning.Context,
    })
}

/* the codes are judged at boot, after the modules declared theirs; the syntax of the list was judged when the configuration was built */
func (instance *Application) refuseUnknownCliBootWarningCodes() {
    registry := instance.bootWarningRegistryOf()

    for index, code := range instance.configuration.Cli().BootWarningsIgnored() {
        context := exceptioncontract.Context{
            "environmentKey": config.CliBootWarningsIgnoredKey,
            "index":          index,
            "code":           code,
            "cliCodes":       registry.CliNames(),
        }

        if false == registry.Known(code) {
            exception.Panic(exception.NewError("cli boot warning code names no boot warning the framework or a module writes", context, nil))
        }

        if false == registry.WrittenInCli(code) {
            exception.Panic(exception.NewError("cli boot warning code names a boot warning only an http process writes, so a cli process never reads it", context, nil))
        }
    }
}

var _ applicationcontract.BootWarningRegistrar = (*moduleBootWarningRegistrar)(nil)

