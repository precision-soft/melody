package application

import (
    "fmt"
    "strings"

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

/* names keys only, never a value: a retired key may hold a credential; one refusal for all of them, so a deployment is corrected in one pass */
func (instance *Application) refuseRetiredEnvironmentKeys() {
    declarer, isDeclarer := instance.configuration.(configcontract.EnvironmentKeyDeclarer)
    if false == isDeclarer {
        return
    }

    retiredKeys := declarer.RetiredEnvironmentKeys()
    if 0 == len(retiredKeys) {
        return
    }

    exception.Panic(exception.NewError(
        retiredEnvironmentKeysMessage(retiredKeys),
        exceptioncontract.Context{"retiredEnvironmentKeys": retiredEnvironmentKeysContext(retiredKeys)},
        nil,
    ))
}

func retiredEnvironmentKeysContext(retiredKeys []configcontract.RetiredEnvironmentKey) []map[string]string {
    entries := make([]map[string]string, 0, len(retiredKeys))
    for _, retiredKey := range retiredKeys {
        entry := map[string]string{"key": retiredKey.Key, "successor": retiredKey.Successor, "owner": retiredKey.Owner}
        if "" != retiredKey.Note {
            entry["note"] = retiredKey.Note
        }

        entries = append(entries, entry)
    }

    return entries
}

func retiredEnvironmentKeysMessage(retiredKeys []configcontract.RetiredEnvironmentKey) string {
    parts := make([]string, 0, len(retiredKeys))
    for _, retiredKey := range retiredKeys {
        part := "the environment key " + retiredKey.Key + " is retired: rename it to " + retiredKey.Successor
        if "" != retiredKey.Note {
            part += ", and " + retiredKey.Note
        }

        parts = append(parts, part)
    }

    return strings.Join(parts, "; ")
}

func (instance *Application) refuseUnappliedParameterOverrides() {
    declarer, isDeclarer := instance.configuration.(configcontract.EnvironmentKeyDeclarer)
    if false == isDeclarer {
        return
    }

    names := declarer.UnappliedParameterOverrides()
    if 0 == len(names) {
        return
    }

    exception.Panic(exception.NewError(
        "a parameter override names a parameter no framework or module key feeds",
        exceptioncontract.Context{"parameterNames": strings.Join(names, ", ")},
        nil,
    ))
}

/* names keys and parameters only, never a value; one refusal for all of them */
func (instance *Application) refuseOverriddenEnvironmentKeys() {
    declarer, isDeclarer := instance.configuration.(configcontract.EnvironmentKeyDeclarer)
    if false == isDeclarer {
        return
    }

    overriddenKeys := declarer.OverriddenEnvironmentKeys()
    if 0 == len(overriddenKeys) {
        return
    }

    exception.Panic(exception.NewError(
        overriddenEnvironmentKeysMessage(overriddenKeys),
        exceptioncontract.Context{"overriddenEnvironmentKeys": overriddenEnvironmentKeysContext(overriddenKeys)},
        nil,
    ))
}

func overriddenEnvironmentKeysContext(overriddenKeys []configcontract.OverriddenEnvironmentKey) []map[string]string {
    entries := make([]map[string]string, 0, len(overriddenKeys))
    for _, overriddenKey := range overriddenKeys {
        entries = append(entries, map[string]string{"key": overriddenKey.Key, "parameter": overriddenKey.Parameter, "owner": overriddenKey.Owner})
    }

    return entries
}

func overriddenEnvironmentKeysMessage(overriddenKeys []configcontract.OverriddenEnvironmentKey) string {
    parts := make([]string, 0, len(overriddenKeys))
    for _, overriddenKey := range overriddenKeys {
        parts = append(parts, "the environment key "+overriddenKey.Key+" is set and reads nothing: the application overrides "+overriddenKey.Parameter+"; remove the line")
    }

    return strings.Join(parts, "; ")
}

/* after the resolve, so a templated value is judged as its readers get it */
func (instance *Application) refuseModuleEnvironmentKeys() {
    declarer, isDeclarer := instance.configuration.(configcontract.EnvironmentKeyDeclarer)
    if false == isDeclarer {
        return
    }

    if checkErr := declarer.CheckEnvironmentKeys(); nil != checkErr {
        exception.Panic(exception.NewError("a declared environment key is refused", nil, checkErr))
    }
}

func (instance *Application) collectUndeclaredEnvironmentKeys() {
    declarer, isDeclarer := instance.configuration.(configcontract.EnvironmentKeyDeclarer)
    if false == isDeclarer {
        return
    }

    keys := declarer.UndeclaredEnvironmentKeys()
    if 0 == len(keys) {
        return
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
