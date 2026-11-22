package config

import (
    "path/filepath"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func (instance *Configuration) registerDefaultParameters(
    projectDirectory string,
) {
    instance.registerKeylessParameter(KernelProjectDirectory, projectDirectory)
    instance.registerKeylessParameter(KernelLogsDirectory, filepath.Join("%"+KernelProjectDirectory+"%", "var", "log"))
    instance.registerKeylessParameter(KernelCacheDirectory, filepath.Join("%"+KernelProjectDirectory+"%", "var", "cache"))

    for _, key := range frameworkEnvironmentKeys() {
        instance.registerFrameworkEnvironmentKey(key)
    }
}

func (instance *Configuration) registerKeylessParameter(name string, template string) {
    instance.refuseDuplicateDefault(name, name)

    instance.parameters[name] = newParameter(name, template, template, true)
}

func (instance *Configuration) registerFrameworkEnvironmentKey(key configcontract.EnvironmentKey) {
    instance.refuseDuplicateDefault(key.Parameter, key.Key)

    _, isSet := instance.environment.Get(key.Key)

    instance.parameters[key.Parameter] = newParameter(key.Key, environmentPlaceholder(key.Key), nil, false == isSet)

    instance.recordEnvironmentKeyDeclarationLocked(frameworkEnvironmentKeyOwner, key)
}

func (instance *Configuration) refuseDuplicateDefault(name string, environmentKey string) {
    if nil == instance.getInternalParameter(name) {
        return
    }

    exception.Panic(
        exception.NewError(
            "duplicate parameter name when setting defaults",
            map[string]any{
                "parameterName":  name,
                "environmentKey": environmentKey,
            },
            nil,
        ),
    )
}
