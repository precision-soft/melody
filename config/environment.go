package config

import (
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

const (
    EnvDevelopment = "dev"
    EnvProduction  = "prod"

    ModeHttp = "http"
    ModeCli  = "cli"

    DefaultModeKey             = "MELODY_DEFAULT_MODE"
    EnvKey                     = "MELODY_ENV"
    HttpAddressKey             = "MELODY_HTTP_ADDRESS"
    HttpMaxRequestBodyBytesKey = "MELODY_HTTP_MAX_REQUEST_BODY_BYTES"
    CliNameKey                 = "MELODY_CLI_NAME"
    CliDescriptionKey          = "MELODY_CLI_DESCRIPTION"
    LogPathKey                 = "MELODY_LOG_PATH"
    LogLevelKey                = "MELODY_LOG_LEVEL"
    DefaultLocaleKey           = "MELODY_DEFAULT_LOCALE"
    PublicDirKey               = "MELODY_PUBLIC_DIR"
    StaticIndexFileKey         = "MELODY_STATIC_INDEX_FILE"
    StaticEnableCacheKey       = "MELODY_STATIC_ENABLE_CACHE"
    StaticCacheMaxAgeKey       = "MELODY_STATIC_CACHE_MAX_AGE"

    KernelDefaultMode             = "kernel.default_mode"
    KernelEnv                     = "kernel.environment"
    KernelHttpAddress             = "kernel.http_address"
    KernelHttpMaxRequestBodyBytes = "kernel.http.max_request_body_bytes"
    KernelCliName                 = "kernel.cli_name"
    KernelCliDescription          = "kernel.cli_description"
    KernelLogPath                 = "kernel.log_path"
    KernelLogLevel                = "kernel.log_level"
    KernelDefaultLocale           = "kernel.default_locale"
    KernelPublicDir               = "kernel.public_dir"
    KernelStaticIndexFile         = "kernel.static.index_file"
    KernelStaticEnableCache       = "kernel.static.enable_cache"
    KernelStaticCacheMaxAge       = "kernel.static.cache_max_age"

    KernelProjectDir = "kernel.project_dir"
    KernelLogsDir    = "kernel.logs_dir"
    KernelCacheDir   = "kernel.cache_dir"
)

type Environment struct {
    values map[string]string
}

func NewEnvironment(source configcontract.EnvironmentSource) (*Environment, error) {
    if true == internal.IsNilInterface(source) {
        return nil, exception.NewError("environment source is required", nil, nil)
    }

    values, loadErr := source.Load()
    if nil != loadErr {
        return nil, loadErr
    }

    return &Environment{
        values: values,
    }, nil
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
