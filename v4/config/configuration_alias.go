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
    CliBootWarningsIgnoredKey: {
        CliBootWarningsIgnoredKey,
        KernelCliBootWarningsIgnored,
    },
    CliBootWarningsDisabledKey: {
        CliBootWarningsDisabledKey,
        KernelCliBootWarningsDisabled,
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
    HttpSessionMaxSessionsKey: {
        HttpSessionMaxSessionsKey,
        KernelHttpSessionMaxSessions,
    },
    HttpShutdownTimeoutKey: {
        HttpShutdownTimeoutKey,
        KernelHttpShutdownTimeout,
    },
    TeardownTimeoutKey: {
        TeardownTimeoutKey,
        KernelTeardownTimeout,
    },
    ScopeTeardownTimeoutKey: {
        ScopeTeardownTimeoutKey,
        KernelScopeTeardownTimeout,
    },
    ContainerCreationWaitersKey: {
        ContainerCreationWaitersKey,
        KernelContainerCreationWaiters,
    },
    ContainerTeardownSequentialKey: {
        ContainerTeardownSequentialKey,
        KernelContainerTeardownSequential,
    },
    HttpReadTimeoutKey: {
        HttpReadTimeoutKey,
        KernelHttpReadTimeout,
    },
    HttpReadHeaderTimeoutKey: {
        HttpReadHeaderTimeoutKey,
        KernelHttpReadHeaderTimeout,
    },
    HttpWriteTimeoutKey: {
        HttpWriteTimeoutKey,
        KernelHttpWriteTimeout,
    },
    HttpIdleTimeoutKey: {
        HttpIdleTimeoutKey,
        KernelHttpIdleTimeout,
    },
    HttpMaxHeaderBytesKey: {
        HttpMaxHeaderBytesKey,
        KernelHttpMaxHeaderBytes,
    },
    HttpUnavailableRetryAfterKey: {
        HttpUnavailableRetryAfterKey,
        KernelHttpUnavailableRetryAfter,
    },
    HttpSessionEmptyOnOutageKey: {
        HttpSessionEmptyOnOutageKey,
        KernelHttpSessionEmptyOnOutage,
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

/* aliasesOfName lists every name the parameter behind this one answers to: a kernel-aliased key is one *Parameter stored under its MELODY_* key and its kernel.* spelling, a module's declared key one stored under its key and its parameter name, and a template may read either, so the secret propagation scans both. A name in no alias group answers only to itself. */
func (instance *Configuration) aliasesOfName(name string) []string {
    for _, group := range environmentKeyAliasMap {
        for _, aliasName := range group {
            if name == aliasName {
                return group
            }
        }
    }

    if declared, isDeclared := instance.declaredEnvironmentKeys[name]; true == isDeclared {
        return []string{name, declared.declaration.Parameter}
    }

    if feedingKey, isFed := instance.declaredEnvironmentParameters[name]; true == isFed {
        return []string{feedingKey, name}
    }

    return []string{name}
}
