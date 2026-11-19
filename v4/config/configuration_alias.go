package config

import (
    "path/filepath"
    "strconv"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* no rule: the views judge the framework's keys */
func frameworkEnvironmentKeys() []configcontract.EnvironmentKey {
    return []configcontract.EnvironmentKey{
        frameworkEnvironmentKey(DefaultModeKey, KernelDefaultMode, ModeHttp),
        frameworkEnvironmentKey(ProcessRoleKey, KernelProcessRole, RoleAll),
        frameworkEnvironmentKey(EnvironmentNameKey, KernelEnvironment, EnvironmentDevelopment),
        frameworkEnvironmentKey(HttpAddressKey, KernelHttpAddress, ":8080"),
        frameworkEnvironmentKey(CliNameKey, KernelCliName, "melody"),
        frameworkEnvironmentKey(CliDescriptionKey, KernelCliDescription, ""),
        frameworkEnvironmentKey(CliBootWarningsIgnoredKey, KernelCliBootWarningsIgnored, ""),
        frameworkEnvironmentKey(CliBootWarningsDisabledKey, KernelCliBootWarningsDisabled, ""),
        frameworkEnvironmentKey(LogPathKey, KernelLogPath, filepath.Join("%"+KernelLogsDirectory+"%", "%"+KernelEnvironment+"%.log")),
        frameworkEnvironmentKey(LogLevelKey, KernelLogLevel, string(loggingcontract.LevelDebug)),
        frameworkEnvironmentKey(DefaultLocaleKey, KernelDefaultLocale, "en"),
        frameworkEnvironmentKey(PublicDirectoryKey, KernelPublicDirectory, "public"),
        frameworkEnvironmentKey(StaticIndexFileKey, KernelStaticIndexFile, "index.html"),
        frameworkEnvironmentKey(StaticEnableCacheKey, KernelStaticEnableCache, "true"),
        frameworkEnvironmentKey(StaticCacheMaxAgeKey, KernelStaticCacheMaxAge, strconv.Itoa(DefaultStaticCacheMaxAge)),
        frameworkEnvironmentKey(StaticExcludedPathsKey, KernelStaticExcludedPaths, ""),
        frameworkEnvironmentKey(HttpMaxRequestBodyBytesKey, KernelHttpMaxRequestBodyBytes, strconv.Itoa(DefaultHttpMaxRequestBodyBytes)),
        frameworkEnvironmentKey(HttpSessionTtlKey, KernelHttpSessionTtl, DefaultSessionTtl.String()),
        frameworkEnvironmentKey(HttpSessionTombstoneRetentionKey, KernelHttpSessionTombstoneRetention, DefaultSessionTombstoneRetention.String()),
        frameworkEnvironmentKey(HttpSessionStorageTimeoutKey, KernelHttpSessionStorageTimeout, DefaultSessionStorageTimeout.String()),
        frameworkEnvironmentKey(HttpSessionMaxSessionsKey, KernelHttpSessionMaxSessions, strconv.Itoa(DefaultSessionMaxSessions)),
        frameworkEnvironmentKey(HttpShutdownTimeoutKey, KernelHttpShutdownTimeout, DefaultHttpShutdownTimeout.String()),
        /* empty so ApplicationConfig and then the framework default fill them */
        frameworkEnvironmentKey(TeardownTimeoutKey, KernelTeardownTimeout, ""),
        frameworkEnvironmentKey(ScopeTeardownTimeoutKey, KernelScopeTeardownTimeout, ""),
        frameworkEnvironmentKey(ContainerCreationWaitersKey, KernelContainerCreationWaiters, ""),
        frameworkEnvironmentKey(ContainerTeardownSequentialKey, KernelContainerTeardownSequential, ""),
        frameworkEnvironmentKey(HttpReadTimeoutKey, KernelHttpReadTimeout, ""),
        frameworkEnvironmentKey(HttpReadHeaderTimeoutKey, KernelHttpReadHeaderTimeout, ""),
        frameworkEnvironmentKey(HttpWriteTimeoutKey, KernelHttpWriteTimeout, ""),
        frameworkEnvironmentKey(HttpIdleTimeoutKey, KernelHttpIdleTimeout, ""),
        frameworkEnvironmentKey(HttpMaxHeaderBytesKey, KernelHttpMaxHeaderBytes, ""),
        frameworkEnvironmentKey(HttpUnavailableRetryAfterKey, KernelHttpUnavailableRetryAfter, ""),
        frameworkEnvironmentKey(HttpSessionEmptyOnOutageKey, KernelHttpSessionEmptyOnOutage, ""),
    }
}

func frameworkEnvironmentKey(key string, parameter string, defaultText string) configcontract.EnvironmentKey {
    return configcontract.EnvironmentKey{
        Key:       key,
        Parameter: parameter,
        Field:     key,
        Default:   defaultText,
    }
}

var frameworkEnvironmentKeyIndex = indexFrameworkEnvironmentKeys()

func indexFrameworkEnvironmentKeys() map[string]configcontract.EnvironmentKey {
    index := make(map[string]configcontract.EnvironmentKey)
    for _, key := range frameworkEnvironmentKeys() {
        index[key.Key] = key
    }

    return index
}

func environmentPlaceholder(environmentKey string) string {
    return "%env(" + environmentKey + ")%"
}

/* a declared key and its parameter share one *Parameter, so the secret propagation scans both names */
func (instance *Configuration) aliasesOfName(name string) []string {
    /* an overridden parameter and its key are two parameters, so a mark does not hop between them */
    if declared, isDeclared := instance.declaredEnvironmentKeys[name]; true == isDeclared {
        if true == instance.isOverriddenLocked(declared.declaration.Parameter) {
            return []string{name}
        }

        return []string{name, declared.declaration.Parameter}
    }

    if feedingKey, isFed := instance.declaredEnvironmentParameters[name]; true == isFed {
        if true == instance.isOverriddenLocked(name) {
            return []string{name}
        }

        return []string{feedingKey, name}
    }

    return []string{name}
}
