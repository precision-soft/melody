package config

import (
    "context"
    "slices"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

const (
    EnvironmentDevelopment = "dev"
    EnvironmentProduction  = "prod"

    ModeHttp = "http"
    ModeCli  = "cli"

    RoleWeb    = "web"
    RoleWorker = "worker"
    RoleAll    = "all"

    /* HttpSessionCookieSecureAlways is the one value MELODY_HTTP_SESSION_COOKIE_SECURE admits: the key only hardens the cookie */
    HttpSessionCookieSecureAlways = "always"

    DefaultModeKey                   = "MELODY_DEFAULT_MODE"
    ProcessRoleKey                   = "MELODY_PROCESS_ROLE"
    EnvironmentNameKey               = "MELODY_ENV"
    HttpAddressKey                   = "MELODY_HTTP_ADDRESS"
    HttpPublicUrlKey                 = "MELODY_HTTP_PUBLIC_URL"
    HttpMaxRequestBodyBytesKey       = "MELODY_HTTP_MAX_REQUEST_BODY_BYTES"
    HttpSessionTtlKey                = "MELODY_HTTP_SESSION_TTL"
    HttpSessionTombstoneRetentionKey = "MELODY_HTTP_SESSION_TOMBSTONE_RETENTION"
    HttpSessionStorageTimeoutKey     = "MELODY_HTTP_SESSION_STORAGE_TIMEOUT"
    HttpSessionMaxSessionsKey        = "MELODY_HTTP_SESSION_MAX_SESSIONS"
    HttpShutdownTimeoutKey           = "MELODY_HTTP_SHUTDOWN_TIMEOUT"
    HttpReadTimeoutKey               = "MELODY_HTTP_READ_TIMEOUT"
    HttpReadHeaderTimeoutKey         = "MELODY_HTTP_READ_HEADER_TIMEOUT"
    HttpWriteTimeoutKey              = "MELODY_HTTP_WRITE_TIMEOUT"
    HttpIdleTimeoutKey               = "MELODY_HTTP_IDLE_TIMEOUT"
    HttpMaxHeaderBytesKey            = "MELODY_HTTP_MAX_HEADER_BYTES"
    HttpUnavailableRetryAfterKey     = "MELODY_HTTP_UNAVAILABLE_RETRY_AFTER"
    HttpSessionEmptyOnOutageKey      = "MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE"
    HttpSessionCookieSecureKey       = "MELODY_HTTP_SESSION_COOKIE_SECURE"
    TeardownTimeoutKey               = "MELODY_TEARDOWN_TIMEOUT"
    ScopeTeardownTimeoutKey          = "MELODY_SCOPE_TEARDOWN_TIMEOUT"
    ContainerCreationWaitersKey      = "MELODY_CONTAINER_CREATION_WAITERS"
    ContainerTeardownSequentialKey   = "MELODY_CONTAINER_TEARDOWN_SEQUENTIAL"
    CliNameKey                       = "MELODY_CLI_NAME"
    CliDescriptionKey                = "MELODY_CLI_DESCRIPTION"
    CliBootWarningsIgnoredKey        = "MELODY_CLI_BOOT_WARNINGS_IGNORED"
    CliBootWarningsDisabledKey       = "MELODY_CLI_BOOT_WARNINGS_DISABLED"
    LogPathKey                       = "MELODY_LOG_PATH"
    LogLevelKey                      = "MELODY_LOG_LEVEL"
    DefaultLocaleKey                 = "MELODY_DEFAULT_LOCALE"
    PublicDirectoryKey               = "MELODY_PUBLIC_DIRECTORY"
    StaticIndexFileKey               = "MELODY_STATIC_INDEX_FILE"
    StaticCacheDisabledKey           = "MELODY_STATIC_CACHE_DISABLED"
    StaticCacheMaxAgeKey             = "MELODY_STATIC_CACHE_MAX_AGE"
    StaticExcludedPathsKey           = "MELODY_STATIC_EXCLUDED_PATHS"

    KernelDefaultMode                   = "kernel.default_mode"
    KernelProcessRole                   = "kernel.process_role"
    KernelEnvironment                   = "kernel.environment"
    KernelHttpAddress                   = "kernel.http_address"
    KernelHttpPublicUrl                 = "kernel.http.public_url"
    KernelHttpMaxRequestBodyBytes       = "kernel.http.max_request_body_bytes"
    KernelHttpSessionTtl                = "kernel.http.session_ttl"
    KernelHttpSessionTombstoneRetention = "kernel.http.session_tombstone_retention"
    KernelHttpSessionStorageTimeout     = "kernel.http.session_storage_timeout"
    KernelHttpSessionMaxSessions        = "kernel.http.session_max_sessions"
    KernelHttpShutdownTimeout           = "kernel.http.shutdown_timeout"
    KernelHttpReadTimeout               = "kernel.http.read_timeout"
    KernelHttpReadHeaderTimeout         = "kernel.http.read_header_timeout"
    KernelHttpWriteTimeout              = "kernel.http.write_timeout"
    KernelHttpIdleTimeout               = "kernel.http.idle_timeout"
    KernelHttpMaxHeaderBytes            = "kernel.http.max_header_bytes"
    KernelHttpUnavailableRetryAfter     = "kernel.http.unavailable_retry_after"
    KernelHttpSessionEmptyOnOutage      = "kernel.http.session_empty_on_outage"
    KernelHttpSessionCookieSecure       = "kernel.http.session_cookie_secure"
    KernelTeardownTimeout               = "kernel.teardown_timeout"
    KernelScopeTeardownTimeout          = "kernel.scope_teardown_timeout"
    KernelContainerCreationWaiters      = "kernel.container_creation_waiters"
    KernelContainerTeardownSequential   = "kernel.container_teardown_sequential"
    KernelCliName                       = "kernel.cli_name"
    KernelCliDescription                = "kernel.cli_description"
    KernelCliBootWarningsIgnored        = "kernel.cli_boot_warnings_ignored"
    KernelCliBootWarningsDisabled       = "kernel.cli_boot_warnings_disabled"
    KernelLogPath                       = "kernel.log_path"
    KernelLogLevel                      = "kernel.log_level"
    KernelDefaultLocale                 = "kernel.default_locale"
    KernelPublicDirectory               = "kernel.public_directory"
    KernelStaticIndexFile               = "kernel.static.index_file"
    KernelStaticCacheDisabled           = "kernel.static.cache_disabled"
    KernelStaticCacheMaxAge             = "kernel.static.cache_max_age"
    KernelStaticExcludedPaths           = "kernel.static.excluded_paths"

    KernelProjectDirectory = "kernel.project_directory"
    KernelLogsDirectory    = "kernel.logs_directory"
    KernelCacheDirectory   = "kernel.cache_directory"
)

type Environment struct {
    values        map[string]string
    assembledFrom map[string][]string
}

type dotEnvProvenanceReader interface {
    dotEnvAssembledFrom() map[string][]string
}

func NewEnvironment(ctx context.Context, source configcontract.EnvironmentSource) (*Environment, error) {
    if true == internal.IsNilInterface(ctx) {
        return nil, exception.NewError("config.NewEnvironment was handed no context", nil, nil)
    }

    if true == internal.IsNilInterface(source) {
        return nil, exception.NewError("environment source is required", nil, nil)
    }

    values, loadErr := source.Load(ctx)
    if nil != loadErr {
        return nil, loadErr
    }

    assembledFrom := map[string][]string(nil)
    if reader, readsProvenance := source.(dotEnvProvenanceReader); true == readsProvenance {
        assembledFrom = reader.dotEnvAssembledFrom()
    }

    return &Environment{
        values:        values,
        assembledFrom: assembledFrom,
    }, nil
}

func (instance *Environment) keysAssembledFrom(name string) []string {
    if nil == instance {
        return nil
    }

    keys := make([]string, 0)
    for key, referencedKeys := range instance.assembledFrom {
        if true == slices.Contains(referencedKeys, name) {
            keys = append(keys, key)
        }
    }

    return keys
}

func (instance *Environment) All() map[string]string {
    copied := make(map[string]string, len(instance.values))

    for key, value := range instance.values {
        copied[key] = value
    }

    return copied
}

func (instance *Environment) Get(key string) (string, bool) {
    value, exists := instance.values[key]
    if false == exists {
        return "", false
    }

    return value, true
}
