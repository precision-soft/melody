package config

import (
    "regexp"
    "sort"
    "strings"
    "sync"

    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

var (
    /* the optional "default:<fallback>:" prefix marks an environment key whose absence is tolerated: "%env(default::KEY)%" falls back to the empty string and "%env(default:some.parameter:KEY)%" falls back to another parameter. Without the prefix an undefined key stays a hard error, so a plain "%env(KEY)%" never silently degrades to empty. */
    envPlaceholderPattern       = regexp.MustCompile(`%env\((default:([A-Za-z_][A-Za-z0-9_.]*)?:)?([A-Za-z_][A-Za-z0-9_]*)\)%`)
    parameterPlaceholderPattern = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_.]+)%`)
    /* matches anything shaped like an environment placeholder so that a spelling the strict pattern rejects (a "default:" prefix with a single colon, an invalid key) is reported instead of surviving as literal text */
    envPlaceholderShapePattern = regexp.MustCompile(`%env\([^)]*\)%`)
)

const (
    escapedPercentPlaceholder = "\x00PERCENT\x00"
)

func NewConfiguration(
    environment *Environment,
    projectDirectory string,
) (*Configuration, error) {
    if nil == environment {
        return nil, exception.NewError("environment is required", nil, nil)
    }

    logger := logging.EmergencyLogger()

    configuration := &Configuration{
        environment: environment,
        parameters:  make(ParameterMap),
        logger:      logger,
    }

    applyDefaultsErr := configuration.applyDefaults(projectDirectory)
    if nil != applyDefaultsErr {
        return nil, applyDefaultsErr
    }

    applyEnvironmentOverridesErr := configuration.applyEnvironmentOverrides()
    if nil != applyEnvironmentOverridesErr {
        return nil, applyEnvironmentOverridesErr
    }

    resolvePlaceholdersErr := configuration.resolvePlaceholders()
    if nil != resolvePlaceholdersErr {
        return nil, resolvePlaceholdersErr
    }

    validateErr := configuration.validate()
    if nil != validateErr {
        return nil, validateErr
    }

    buildCliConfigurationErr := configuration.buildCliConfiguration()
    if nil != buildCliConfigurationErr {
        return nil, buildCliConfigurationErr
    }

    buildKernelConfigurationErr := configuration.buildKernelConfiguration()
    if nil != buildKernelConfigurationErr {
        return nil, buildKernelConfigurationErr
    }

    buildHttpConfigurationErr := configuration.buildHttpConfiguration()
    if nil != buildHttpConfigurationErr {
        return nil, buildHttpConfigurationErr
    }

    logger.Info("configuration validated", nil)

    return configuration, nil
}

type Configuration struct {
    mutex       sync.RWMutex
    environment *Environment
    parameters  ParameterMap
    logger      loggingcontract.Logger
    cli         *cliConfiguration
    kernel      *kernelConfiguration
    http        *httpConfiguration

    /* parameters whose raw value escaped a literal percent with %%; after resolution their value legitimately contains %...%, which the unresolved-placeholder check must not mistake for a placeholder it failed to expand */
    parametersWithEscapedPercents map[string]bool
}

func (instance *Configuration) Cli() configcontract.CliConfiguration {
    return instance.cli
}

func (instance *Configuration) Kernel() configcontract.KernelConfiguration {
    return instance.kernel
}

func (instance *Configuration) Http() configcontract.HttpConfiguration {
    return instance.http
}

func (instance *Configuration) Parameters() ParameterMap {
    /* @important read under the read lock because RegisterRuntime mutates the shared parameters map at runtime; an unguarded range here races the writer and trips Go's fatal "concurrent map read and map write" */
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return internal.CopyStringMap[*Parameter](
        instance.parameters,
    )
}

/* projectDirectoryParameterValue reads the project-directory default for diagnostics without requiring a resolved configuration. */
func (instance *Configuration) projectDirectoryParameterValue() string {
    parameter := instance.Get(KernelProjectDir)
    if nil == parameter {
        return ""
    }

    return parameter.String()
}

func (instance *Configuration) Get(name string) configcontract.Parameter {
    /* @important read under the read lock because RegisterRuntime mutates the shared parameters map at runtime; an unguarded read here races the writer and trips Go's fatal "concurrent map read and map write" */
    instance.mutex.RLock()
    parameter := instance.getInternalParameter(name)
    instance.mutex.RUnlock()

    if nil == parameter {
        return nil
    }

    return parameter
}

func (instance *Configuration) MustGet(name string) configcontract.Parameter {
    parameter := instance.Get(name)
    if nil == parameter {
        exception.Panic(
            exception.NewError(
                "parameter does not exist",
                exceptioncontract.Context{
                    "parameterName": name,
                },
                nil,
            ),
        )
    }

    return parameter
}

func (instance *Configuration) RegisterRuntime(name string, value any) {
    instance.registerRuntimeParameter(name, value, false)
}

/* RegisterRuntimeSecret registers a parameter that holds a credential, so that the commands which render the configuration redact it. The value is stored and resolved like any other: the marking governs display, not storage, and it travels to every parameter whose template reads this one. */
func (instance *Configuration) RegisterRuntimeSecret(name string, value any) {
    instance.registerRuntimeParameter(name, value, true)
}

func (instance *Configuration) registerRuntimeParameter(name string, value any, isSecret bool) {
    if "" == name {
        exception.Panic(
            exception.NewError("cannot register parameters with empty names", nil, nil),
        )
    }

    if true == instance.isReserved(name) {
        exception.Panic(
            exception.NewError(
                "cannot register parameters with reserved prefix",
                exceptioncontract.Context{
                    "parameterName": name,
                },
                nil,
            ),
        )
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* @important use the lock-free lookup here, not Get: Get now takes the read lock and sync.RWMutex is non-reentrant, so self-calling it while holding the write lock would deadlock */
    existingParameter := instance.getInternalParameter(name)
    if nil != existingParameter {
        exception.Panic(
            exception.NewError(
                "duplicate parameter name when adding runtime parameter",
                exceptioncontract.Context{
                    "parameterName": name,
                },
                nil,
            ),
        )
    }

    parameter := NewParameter("", value, value, false)
    parameter.isSecret = isSecret

    instance.parameters[name] = parameter
}

/* MarkSecret marks an already registered parameter as holding a credential. The parameters melody registers automatically from the .env artifacts are the ones most likely to hold one, and they exist before any module runs, so marking them is separate from declaring them.

An absent parameter is left alone rather than reported: an environment key is legitimately undefined in some environments, and refusing to boot over one would make the marking unusable exactly where it matters. The secret column of melody:debug:parameters is what confirms a marking took effect. */
func (instance *Configuration) MarkSecret(name string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    parameter := instance.getInternalParameter(name)
    if nil == parameter {
        return false
    }

    parameter.isSecret = true

    return true
}

func (instance *Configuration) Names() []string {
    /* @important read under the read lock because RegisterRuntime mutates the shared parameters map at runtime; an unguarded range here races the writer and trips Go's fatal "concurrent map read and map write" */
    instance.mutex.RLock()

    names := make([]string, 0, len(instance.parameters))

    for name := range instance.parameters {
        names = append(names, name)
    }

    instance.mutex.RUnlock()

    sort.Strings(names)

    return names
}

func (instance *Configuration) applyDefaults(projectDirectory string) error {
    instance.registerDefaultParameters(projectDirectory)

    instance.logger.Info(
        "configuration defaults applied",
        loggingcontract.Context{
            "projectDirectory": projectDirectory,
        },
    )

    return nil
}

func (instance *Configuration) applyEnvironmentOverrides() error {
    registerEnvironmentParametersErr := instance.registerEnvironmentParameters()
    if nil != registerEnvironmentParametersErr {
        return exception.NewError(
            "could not initialize the environment parameters",
            nil,
            registerEnvironmentParametersErr,
        )
    }

    instance.logger.Info(
        "configuration environment overrides applied",
        loggingcontract.Context{
            "environmentKeys": len(instance.environment.All()),
        },
    )

    /* zero keys almost always means the .env artifacts were not found rather than deliberately empty: melody derives the project directory from the executable location (the working directory under go run), so a binary executed outside its project directory silently sees no .env and later fails resolve with an unsuggestive "undefined environment key". Name the directory that was searched so the cause is visible. */
    if 0 == len(instance.environment.All()) {
        instance.logger.Warning(
            "no environment keys were loaded from the .env artifacts; melody derives the project directory from the executable location (the working directory under go run), so a binary run from elsewhere does not find its .env files",
            loggingcontract.Context{
                "projectDirectory": instance.projectDirectoryParameterValue(),
            },
        )
    }

    return nil
}

func (instance *Configuration) resolvePlaceholders() error {
    resolveErr := instance.Resolve()
    if nil != resolveErr {
        return exception.NewError("could not resolve the config parameters", nil, resolveErr)
    }

    instance.logger.Info("configuration parameters resolved", nil)

    return nil
}

func (instance *Configuration) buildCliConfiguration() error {
    cliConfigurationInstance, newCliConfigurationErr := newCliConfiguration(
        instance.MustGet(CliNameKey).MustString(),
        instance.MustGet(CliDescriptionKey).MustString(),
    )
    if nil != newCliConfigurationErr {
        return exception.NewError("could not initialize the cli configuration", nil, newCliConfigurationErr)
    }

    instance.cli = cliConfigurationInstance

    instance.logger.Info("configuration cli initialized", nil)

    return nil
}

func (instance *Configuration) buildKernelConfiguration() error {
    kernelConfigurationInstance, newKernelConfigurationErr := newKernelConfiguration(
        instance.MustGet(KernelDefaultMode).MustString(),
        instance.MustGet(KernelProcessRole).MustString(),
        instance.MustGet(KernelEnv).MustString(),
        instance.MustGet(KernelProjectDir).MustString(),
        instance.MustGet(KernelLogsDir).MustString(),
        instance.MustGet(KernelLogPath).MustString(),
        instance.MustGet(KernelLogLevel).MustString(),
        instance.MustGet(KernelCacheDir).MustString(),
    )
    if nil != newKernelConfigurationErr {
        return exception.NewError("could not initialize the kernel configuration", nil, newKernelConfigurationErr)
    }

    instance.kernel = kernelConfigurationInstance

    instance.logger.Info("configuration kernel initialized", nil)

    return nil
}

func (instance *Configuration) buildHttpConfiguration() error {
    httpMaxRequestBodyBytes, httpMaxRequestBodyBytesErr := instance.MustGet(KernelHttpMaxRequestBodyBytes).Int()
    if nil != httpMaxRequestBodyBytesErr {
        return exception.NewError(
            "invalid environment value",
            exceptioncontract.Context{
                "environmentKey": HttpMaxRequestBodyBytesKey,
            },
            httpMaxRequestBodyBytesErr,
        )
    }

    staticCacheMaxAge, staticCacheMaxAgeErr := instance.MustGet(KernelStaticCacheMaxAge).Int()
    if nil != staticCacheMaxAgeErr {
        return exception.NewError(
            "invalid environment value",
            exceptioncontract.Context{
                "environmentKey": StaticCacheMaxAgeKey,
            },
            staticCacheMaxAgeErr,
        )
    }

    staticEnableCache, staticEnableCacheErr := instance.MustGet(KernelStaticEnableCache).Bool()
    if nil != staticEnableCacheErr {
        return exception.NewError(
            "invalid environment value",
            exceptioncontract.Context{
                "environmentKey": StaticEnableCacheKey,
            },
            staticEnableCacheErr,
        )
    }

    httpConfigurationInstance, newHttpConfigurationErr := newHttpConfiguration(
        instance.MustGet(KernelHttpAddress).MustString(),
        instance.MustGet(KernelDefaultLocale).MustString(),
        instance.MustGet(KernelPublicDir).MustString(),
        instance.MustGet(KernelStaticIndexFile).MustString(),
        httpMaxRequestBodyBytes,
        staticEnableCache,
        staticCacheMaxAge,
    )
    if nil != newHttpConfigurationErr {
        return exception.NewError("could not initialize the http configuration", nil, newHttpConfigurationErr)
    }

    instance.http = httpConfigurationInstance

    instance.logger.Info("configuration http initialized", nil)

    return nil
}

func (instance *Configuration) registerEnvironmentParameters() error {
    environment := instance.environment.All()

    for environmentKey, environmentValue := range environment {
        if true == instance.isReserved(environmentKey) {
            return exception.NewError(
                "environment key uses reserved parameter prefix",
                exceptioncontract.Context{
                    "environmentKey": environmentKey,
                },
                nil,
            )
        }

        parameterNames := instance.mapEnvironmentKeyToParameterNames(environmentKey)
        if nil == parameterNames || 0 == len(parameterNames) {
            continue
        }

        addAliasedParameterFromEnvironmentErr := instance.addAliasedParameterFromEnvironment(
            parameterNames,
            environmentKey,
            environmentValue,
        )
        if nil != addAliasedParameterFromEnvironmentErr {
            return addAliasedParameterFromEnvironmentErr
        }
    }

    return nil
}

func (instance *Configuration) isReserved(name string) bool {
    return strings.HasPrefix(name, "kernel.")
}

func (instance *Configuration) escapePercents(value string) string {
    if "" == value {
        return value
    }

    return strings.ReplaceAll(value, "%%", escapedPercentPlaceholder)
}

func (instance *Configuration) unescapePercents(value string) string {
    if "" == value {
        return value
    }

    return strings.ReplaceAll(value, escapedPercentPlaceholder, "%")
}

/* @important getInternalParameter is the lock-free map lookup primitive; it must NOT take the lock because it is called both at single-threaded construction (placeholder resolution) and while the write lock is already held (RegisterRuntime). Concurrent readers go through Get/Parameters/Names, which take the read lock around it. */
func (instance *Configuration) getInternalParameter(name string) *Parameter {
    parameter, exists := instance.parameters[name]
    if false == exists || nil == parameter {
        return nil
    }

    return parameter
}

var _ configcontract.Configuration = (*Configuration)(nil)
