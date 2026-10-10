package config

var environmentKeyAliasMap = map[string][]string{
    DefaultModeKey: {
        DefaultModeKey,
        KernelDefaultMode,
    },
    ProcessRoleKey: {
        ProcessRoleKey,
        KernelProcessRole,
    },
    EnvKey: {
        EnvKey,
        KernelEnv,
    },
    HttpAddressKey: {
        HttpAddressKey,
        KernelHttpAddress,
    },
    CliNameKey: {
        CliNameKey,
        KernelCliName,
    },
    CliDescriptionKey: {
        CliDescriptionKey,
        KernelCliDescription,
    },
    LogPathKey: {
        LogPathKey,
        KernelLogPath,
    },
    LogLevelKey: {
        LogLevelKey,
        KernelLogLevel,
    },
    DefaultLocaleKey: {
        DefaultLocaleKey,
        KernelDefaultLocale,
    },
    PublicDirKey: {
        PublicDirKey,
        KernelPublicDir,
    },
    StaticIndexFileKey: {
        StaticIndexFileKey,
        KernelStaticIndexFile,
    },
    StaticEnableCacheKey: {
        StaticEnableCacheKey,
        KernelStaticEnableCache,
    },
    StaticCacheMaxAgeKey: {
        StaticCacheMaxAgeKey,
        KernelStaticCacheMaxAge,
    },
    StaticExcludedPathsKey: {
        StaticExcludedPathsKey,
        KernelStaticExcludedPaths,
    },
    HttpMaxRequestBodyBytesKey: {
        HttpMaxRequestBodyBytesKey,
        KernelHttpMaxRequestBodyBytes,
    },
    HttpSessionTtlKey: {
        HttpSessionTtlKey,
        KernelHttpSessionTtl,
    },
    HttpSessionTombstoneRetentionKey: {
        HttpSessionTombstoneRetentionKey,
        KernelHttpSessionTombstoneRetention,
    },
    HttpShutdownTimeoutKey: {
        HttpShutdownTimeoutKey,
        KernelHttpShutdownTimeout,
    },
}

func (instance *Configuration) addAliasedParameterFromEnvironment(
    parameterNames []string,
    environmentKey string,
    environmentValue string,
) error {
    if nil == parameterNames || 0 == len(parameterNames) {
        return nil
    }

    parameterInstance := NewParameter(
        environmentKey,
        environmentValue,
        environmentValue,
        false,
    )

    for _, name := range parameterNames {
        if "" == name {
            continue
        }

        instance.parameters[name] = parameterInstance
    }

    return nil
}

func (instance *Configuration) mapEnvironmentKeyToParameterNames(
    environmentKey string,
) []string {
    parameterNames, exists := environmentKeyAliasMap[environmentKey]
    if true == exists {
        return parameterNames
    }

    return []string{
        environmentKey,
    }
}

/* aliasesOfName lists every name the parameter behind this one answers to: a kernel-aliased key is one *Parameter stored under its MELODY_* key and its kernel.* spelling, and a template may read either, so the secret propagation scans both. A name in no alias group answers only to itself. */
func aliasesOfName(name string) []string {
    for _, group := range environmentKeyAliasMap {
        for _, aliasName := range group {
            if name == aliasName {
                return group
            }
        }
    }

    return []string{name}
}
