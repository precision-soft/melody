package config

import (
    "fmt"
    "path/filepath"
    "strconv"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/logging"
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
        frameworkEnvironmentKey(BootWarningsIgnoredKey, KernelBootWarningsIgnored, ""),
        frameworkEnvironmentKey(LogPathKey, KernelLogPath, filepath.Join("%"+KernelLogsDirectory+"%", "%"+KernelEnvironment+"%.log")),
        frameworkEnvironmentKey(LogLevelKey, KernelLogLevel, string(loggingcontract.LevelDebug)),
        frameworkEnvironmentKey(LogFileModeKey, KernelLogFileMode, fmt.Sprintf("%#o", uint32(logging.DefaultLogFileMode))),
        frameworkEnvironmentKey(LogDirectoryModeKey, KernelLogDirectoryMode, fmt.Sprintf("%#o", uint32(logging.DefaultLogDirectoryMode))),
        frameworkEnvironmentKey(CacheDirectoryModeKey, KernelCacheDirectoryMode, fmt.Sprintf("%#o", uint32(DefaultCacheDirectoryMode))),
        frameworkEnvironmentKey(DefaultLocaleKey, KernelDefaultLocale, "en"),
        frameworkEnvironmentKey(PublicDirectoryKey, KernelPublicDirectory, "public"),
        frameworkEnvironmentKey(StaticIndexFileKey, KernelStaticIndexFile, "index.html"),
        frameworkEnvironmentKey(StaticCacheDisabledKey, KernelStaticCacheDisabled, "false"),
        frameworkEnvironmentKey(StaticCacheMaxAgeKey, KernelStaticCacheMaxAge, strconv.Itoa(DefaultStaticCacheMaxAge)),
        frameworkEnvironmentKey(StaticExcludedPathsKey, KernelStaticExcludedPaths, ""),
        frameworkEnvironmentKey(HttpMaxRequestBodyBytesKey, KernelHttpMaxRequestBodyBytes, strconv.Itoa(DefaultHttpMaxRequestBodyBytes)),
        frameworkEnvironmentKey(HttpSessionTtlKey, KernelHttpSessionTtl, DefaultSessionTtl.String()),
        frameworkEnvironmentKey(HttpSessionTombstoneRetentionKey, KernelHttpSessionTombstoneRetention, DefaultSessionTombstoneRetention.String()),
        frameworkEnvironmentKey(HttpSessionStorageTimeoutKey, KernelHttpSessionStorageTimeout, DefaultSessionStorageTimeout.String()),
        frameworkEnvironmentKey(HttpSessionMaxSessionsKey, KernelHttpSessionMaxSessions, strconv.Itoa(DefaultSessionMaxSessions)),
        /* empty so ApplicationConfig and then the framework default fill them */
        frameworkEnvironmentKey(HttpShutdownTimeoutKey, KernelHttpShutdownTimeout, ""),
        frameworkEnvironmentKey(TeardownTimeoutKey, KernelTeardownTimeout, ""),
        frameworkEnvironmentKey(ScopeTeardownTimeoutKey, KernelScopeTeardownTimeout, ""),
        frameworkEnvironmentKey(ContainerCreationWaitersKey, KernelContainerCreationWaiters, ""),
        frameworkEnvironmentKey(ContainerTeardownSequentialKey, KernelContainerTeardownSequential, ""),
        frameworkEnvironmentKey(HttpPublicUrlKey, KernelHttpPublicUrl, ""),
        frameworkEnvironmentKey(HttpReadTimeoutKey, KernelHttpReadTimeout, ""),
        frameworkEnvironmentKey(HttpReadHeaderTimeoutKey, KernelHttpReadHeaderTimeout, ""),
        frameworkEnvironmentKey(HttpWriteTimeoutKey, KernelHttpWriteTimeout, ""),
        frameworkEnvironmentKey(HttpIdleTimeoutKey, KernelHttpIdleTimeout, ""),
        frameworkEnvironmentKey(HttpMaxHeaderBytesKey, KernelHttpMaxHeaderBytes, ""),
        frameworkEnvironmentKey(HttpUnavailableRetryAfterKey, KernelHttpUnavailableRetryAfter, ""),
        frameworkEnvironmentKey(HttpSessionEmptyOnOutageKey, KernelHttpSessionEmptyOnOutage, ""),
        frameworkEnvironmentKey(HttpSessionCookieSecureKey, KernelHttpSessionCookieSecure, ""),
        frameworkEnvironmentKey(NamespaceKey, KernelNamespace, ""),
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
