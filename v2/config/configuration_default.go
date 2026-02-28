package config

import (
    "path/filepath"

    "github.com/precision-soft/melody/v2/exception"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

func (instance *Configuration) registerDefaultParameters(
    projectDirectory string,
) {
    instance.setDefaultParameter(KernelProjectDir, projectDirectory)
    instance.setDefaultParameter(KernelLogsDir, filepath.Join("%"+KernelProjectDir+"%", "var", "log"))
    instance.setDefaultParameter(KernelCacheDir, filepath.Join("%"+KernelProjectDir+"%", "var", "cache"))

    instance.setDefaultParameter(DefaultModeKey, ModeHttp)
    instance.setDefaultParameter(EnvKey, EnvDevelopment)

    instance.setDefaultParameter(HttpAddressKey, ":8080")
    instance.setDefaultParameter(HttpMaxRequestBodyBytesKey, 1048576)

    instance.setDefaultParameter(CliNameKey, "melody")

    instance.setDefaultParameter(CliDescriptionKey, "")

    instance.setDefaultParameter(LogPathKey, filepath.Join("%"+KernelLogsDir+"%", "%"+KernelEnv+"%.log"))
    instance.setDefaultParameter(LogLevelKey, string(loggingcontract.LevelDebug))

    instance.setDefaultParameter(DefaultLocaleKey, "en")

    instance.setDefaultParameter(PublicDirKey, "public")

    instance.setDefaultParameter(StaticIndexFileKey, "index.html")
    instance.setDefaultParameter(StaticEnableCacheKey, true)
    instance.setDefaultParameter(StaticCacheMaxAgeKey, 3600)
}

func (instance *Configuration) setDefaultParameter(
    environmentKey string,
    defaultValue any,
) {
    parameterNames := instance.mapEnvironmentKeyToParameterNames(environmentKey)
    if nil == parameterNames || 0 == len(parameterNames) {
        return
    }

    parameter := NewParameter(
        environmentKey,
        defaultValue,
        defaultValue,
        true,
    )

    for _, name := range parameterNames {
        if "" == name {
            continue
        }

        existingParameter := instance.Get(name)
        if nil != existingParameter {
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

        instance.parameters[name] = parameter
    }
}
