package config

import (
    "sort"
    "strings"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* sorted, so the same override refuses on every boot; a kernel.* one is applied before the views are built, any other waits for the declaration of the key that feeds it */
func (instance *Configuration) applyParameterOverrides(overrides map[string]string) error {
    names := make([]string, 0, len(overrides))
    for name := range overrides {
        names = append(names, name)
    }
    sort.Strings(names)

    for _, name := range names {
        context := exceptioncontract.Context{
            "parameterName": name,
        }

        if "" == strings.TrimSpace(name) || name != strings.TrimSpace(name) {
            return exception.NewError("a parameter override names an empty or padded parameter", context, nil)
        }

        if KernelEnvironment == name {
            return exception.NewError("kernel.environment cannot be overridden: it selected the .env.<name> files before the configuration existed; set MELODY_ENV in .env or .env.local", context, nil)
        }

        if KernelProjectDirectory == name {
            return exception.NewError("kernel.project_directory cannot be overridden: it is the path anchor the .env artifacts were read from", context, nil)
        }

        if feedingKey, isFed := instance.declaredEnvironmentParameters[name]; true == isFed && applicationEnvironmentKeyOwner == instance.declaredEnvironmentKeys[feedingKey].owner {
            context["environmentKey"] = feedingKey

            return exception.NewError("the application overrides the parameter of its own environment key; write the template as the key's Default", context, nil)
        }

        if false == instance.isReserved(name) {
            if nil == instance.pendingParameterOverrides {
                instance.pendingParameterOverrides = make(map[string]string)
            }

            instance.pendingParameterOverrides[name] = overrides[name]

            continue
        }

        existing := instance.getInternalParameter(name)
        if nil == existing {
            return exception.NewError("a parameter override names a kernel parameter that does not exist", context, nil)
        }

        instance.parameters[name] = newOverriddenParameter(existing.environmentKey, name, overrides[name])
    }

    return nil
}

/* it keeps the key it stopped reading, which debug:parameters and the refusals name */
func newOverriddenParameter(environmentKey string, name string, template string) *Parameter {
    parameter := newParameter(environmentKey, template, nil, false)
    parameter.name = name
    parameter.overridden = true

    return parameter
}

func (instance *Configuration) isOverriddenLocked(name string) bool {
    parameter := instance.getInternalParameter(name)

    return nil != parameter && true == parameter.overridden
}

func (instance *Configuration) OverriddenEnvironmentKeys() []configcontract.OverriddenEnvironmentKey {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    overriddenKeys := make([]configcontract.OverriddenEnvironmentKey, 0)
    if nil == instance.environment {
        return overriddenKeys
    }

    for key, declared := range instance.declaredEnvironmentKeys {
        parameter := instance.getInternalParameter(declared.declaration.Parameter)
        if nil == parameter || false == parameter.overridden {
            continue
        }

        if _, isSet := instance.environment.Get(key); false == isSet {
            continue
        }

        if template, isString := parameter.template.(string); true == isString && true == templateReads(template, key, true) {
            continue
        }

        overriddenKeys = append(overriddenKeys, configcontract.OverriddenEnvironmentKey{
            Key:       key,
            Parameter: declared.declaration.Parameter,
            Owner:     declared.owner,
        })
    }

    sort.Slice(overriddenKeys, func(left int, right int) bool {
        return overriddenKeys[left].Key < overriddenKeys[right].Key
    })

    return overriddenKeys
}

func (instance *Configuration) UnappliedParameterOverrides() []string {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    names := make([]string, 0, len(instance.pendingParameterOverrides))
    for name := range instance.pendingParameterOverrides {
        names = append(names, name)
    }
    sort.Strings(names)

    return names
}

func (instance *Configuration) EnvironmentKeyParameter(key string) (string, bool) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    declared, isDeclared := instance.declaredEnvironmentKeys[key]
    if false == isDeclared {
        return "", false
    }

    return declared.declaration.Parameter, true
}
