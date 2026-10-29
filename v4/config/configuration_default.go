package config

import (
    "path/filepath"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func (instance *Configuration) registerDefaultParameters(
    projectDirectory string,
) {
    instance.setDefaultParameter(KernelProjectDir, projectDirectory)
    instance.setDefaultParameter(KernelLogsDir, filepath.Join("%"+KernelProjectDir+"%", "var", "log"))
    instance.setDefaultParameter(KernelCacheDir, filepath.Join("%"+KernelProjectDir+"%", "var", "cache"))

    instance.setDefaultParameter(DefaultModeKey, ModeHttp)
    instance.setDefaultParameter(ProcessRoleKey, RoleAll)
    instance.setDefaultParameter(EnvKey, EnvDevelopment)

    instance.setDefaultParameter(HttpAddressKey, ":8080")
    instance.setDefaultParameter(HttpMaxRequestBodyBytesKey, DefaultHttpMaxRequestBodyBytes)

    instance.setDefaultParameter(HttpSessionTtlKey, DefaultSessionTtl.String())

    instance.setDefaultParameter(HttpSessionTombstoneRetentionKey, DefaultSessionTombstoneRetention.String())

    instance.setDefaultParameter(HttpShutdownTimeoutKey, DefaultHttpShutdownTimeout.String())

    /* absent unless a deployment sets them, so ApplicationConfig and then the framework default fill them */
    instance.setDefaultParameter(TeardownTimeoutKey, "")
    instance.setDefaultParameter(HttpReadTimeoutKey, "")
    instance.setDefaultParameter(HttpReadHeaderTimeoutKey, "")
    instance.setDefaultParameter(HttpWriteTimeoutKey, "")
    instance.setDefaultParameter(HttpIdleTimeoutKey, "")
    instance.setDefaultParameter(HttpMaxHeaderBytesKey, "")

    instance.setDefaultParameter(CliNameKey, "melody")

    instance.setDefaultParameter(CliDescriptionKey, "")

    instance.setDefaultParameter(LogPathKey, filepath.Join("%"+KernelLogsDir+"%", "%"+KernelEnv+"%.log"))
    instance.setDefaultParameter(LogLevelKey, string(loggingcontract.LevelDebug))

    instance.setDefaultParameter(DefaultLocaleKey, "en")

    instance.setDefaultParameter(PublicDirKey, "public")

    instance.setDefaultParameter(StaticIndexFileKey, "index.html")
    instance.setDefaultParameter(StaticEnableCacheKey, true)
    instance.setDefaultParameter(StaticCacheMaxAgeKey, DefaultStaticCacheMaxAge)

    /* an empty list keeps the built-in file server answering for every path it recognizes */
    instance.setDefaultParameter(StaticExcludedPathsKey, "")
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
        if false == internal.IsNilInterface(existingParameter) {
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
