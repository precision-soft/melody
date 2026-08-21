package config

import (
    "path/filepath"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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
    instance.setDefaultParameter(HttpMaxRequestBodyBytesKey, 1048576)

    /* the default is zero — no expiry — because that is what every deployment predating the setting already had, and picking a bound here would start logging users out at a lifetime nobody chose; the unbounded-growth hazard of the in-memory storage is answered by the boot warning, not by a quiet default. The full reasoning lives on DefaultSessionTtl. */
    instance.setDefaultParameter(HttpSessionTtlKey, DefaultSessionTtl.String())

    instance.setDefaultParameter(CliNameKey, "melody")

    instance.setDefaultParameter(CliDescriptionKey, "")

    instance.setDefaultParameter(LogPathKey, filepath.Join("%"+KernelLogsDir+"%", "%"+KernelEnv+"%.log"))
    instance.setDefaultParameter(LogLevelKey, string(loggingcontract.LevelDebug))

    instance.setDefaultParameter(DefaultLocaleKey, "en")

    instance.setDefaultParameter(PublicDirKey, "public")

    instance.setDefaultParameter(StaticIndexFileKey, "index.html")
    instance.setDefaultParameter(StaticEnableCacheKey, true)
    instance.setDefaultParameter(StaticCacheMaxAgeKey, 3600)

    /* an empty list keeps the built-in file server answering for every path it recognizes, which is what melody has always served */
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
