package application

import (
    "fmt"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* first, so a hand registration of a declared parameter is the duplicate of the aggregated report */
func (instance *Application) bootModuleEnvironmentKeys() {
    for _, moduleInstance := range instance.modules {
        environmentKeyModule, ok := moduleInstance.(applicationcontract.EnvironmentKeyModule)
        if false == ok {
            continue
        }

        declarer, isDeclarer := instance.configuration.(configcontract.EnvironmentKeyDeclarer)
        if false == isDeclarer {
            exception.Panic(exception.NewError(
                "the configuration cannot hold the environment keys a module declares",
                exceptioncontract.Context{
                    "module":            moduleInstance.Name(),
                    "configurationType": fmt.Sprintf("%T", instance.configuration),
                },
                nil,
            ))
        }

        environmentKeyModule.RegisterEnvironmentKeys(&moduleEnvironmentKeyRegistrar{
            declarer:   declarer,
            moduleName: moduleInstance.Name(),
        })
    }
}

type moduleEnvironmentKeyRegistrar struct {
    declarer   configcontract.EnvironmentKeyDeclarer
    moduleName string
}

func (instance *moduleEnvironmentKeyRegistrar) DeclareEnvironmentKey(key configcontract.EnvironmentKey) {
    if declareErr := instance.declarer.DeclareEnvironmentKey(instance.moduleName, key); nil != declareErr {
        exception.Panic(exception.NewError(
            "a module's environment key declaration is refused",
            exceptioncontract.Context{"module": instance.moduleName, "environmentKey": key.Key},
            declareErr,
        ))
    }
}

/* after the resolve, so a templated value is judged as its readers get it */
func (instance *Application) refuseModuleEnvironmentKeys() {
    declarer, isDeclarer := instance.configuration.(configcontract.EnvironmentKeyDeclarer)
    if false == isDeclarer {
        return
    }

    if checkErr := declarer.CheckEnvironmentKeys(); nil != checkErr {
        exception.Panic(exception.NewError("a module's environment key is refused", nil, checkErr))
    }
}

/* marked secret: the key of a module this process does not register may hold a credential */
func (instance *Application) collectUndeclaredEnvironmentKeys() {
    declarer, isDeclarer := instance.configuration.(configcontract.EnvironmentKeyDeclarer)
    if false == isDeclarer {
        return
    }

    keys := declarer.UndeclaredEnvironmentKeys()
    if 0 == len(keys) {
        return
    }

    for _, key := range keys {
        instance.configuration.MarkSecret(key)
    }

    instance.bootWarnings = append(instance.bootWarnings, internal.BootWarning{
        Name:    internal.BootWarningNameEnvironmentUndeclaredKey,
        Message: "an environment key in the MELODY_ namespace is neither the framework's nor declared by a registered module; it may be misspelt, or belong to a module this process does not register, and its value is redacted",
        Context: loggingcontract.Context{
            "environmentKeys": keys,
        },
    })
}

var _ applicationcontract.EnvironmentKeyRegistrar = (*moduleEnvironmentKeyRegistrar)(nil)
