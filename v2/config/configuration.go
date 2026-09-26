package config

import (
    "regexp"
    "sort"
    "strings"
    "sync"
    "sync/atomic"

    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

var (
    /* the optional "default:<fallback>:" prefix marks an environment key whose absence is tolerated: "%env(default::KEY)%" falls back to the empty string and "%env(default:some.parameter:KEY)%" falls back to another parameter. Without the prefix an undefined key stays a hard error, so a plain "%env(KEY)%" never silently degrades to empty. */
    envPlaceholderPattern = regexp.MustCompile(`%env\((default:([A-Za-z_][A-Za-z0-9_.]*)?:)?([A-Za-z_][A-Za-z0-9_]*)\)%`)
    /* a single-character name is a valid reference, since the default processor's fallback group accepts one; otherwise %a% would survive as literal text */
    parameterPlaceholderPattern = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_.]*)%`)
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

    /* set once the boot-time Resolve has run, so a parameter registered afterwards is resolved on registration instead of keeping its raw template */
    resolved bool

    /* written and read under the configuration write lock, so a MarkServing racing a Resolve either waits for the rewrite or lands first and the rewrite is refused; the atomic wrapper is for a lock-free reader, not the synchronization */
    serving atomic.Bool
}

/* MarkServing records that the wiring phase is over and the application has started running. From that point Resolve is refused: services built during boot hold the values they read, so re-resolving reconfigures nothing and only rewrites the parameter store under readers that expect it settled. Registering a parameter still works — it resolves itself on registration — which is what keeps a late module functioning. */
func (instance *Configuration) MarkServing() {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.serving.Store(true)
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
    /* read under the read lock: RegisterRuntime mutates the parameters map at runtime */
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
    /* read under the read lock: RegisterRuntime mutates the parameters map at runtime */
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

/* RegisterRuntimeSecret registers a parameter that holds a credential, so that the commands which render the configuration redact it. The value is stored and resolved like any other: the marking governs display, not storage, and it travels to every parameter whose template reads this one. It does not travel backwards: the parameter melody auto-registered from the environment key this template reads holds the same credential and needs its own MarkSecret. */
func (instance *Configuration) RegisterRuntimeSecret(name string, value any) {
    instance.registerRuntimeParameter(name, value, true)
}

func (instance *Configuration) registerRuntimeParameter(name string, value any, isSecret bool) {
    if "" == strings.TrimSpace(name) {
        exception.Panic(
            exception.NewError("cannot register parameters with empty names", nil, nil),
        )
    }

    /* the name is judged trimmed: Get is an exact map lookup, so a padded name would register a parameter no lookup reaches */
    if name != strings.TrimSpace(name) {
        exception.Panic(
            exception.NewError(
                "cannot register parameters with surrounding whitespace in the name",
                exceptioncontract.Context{
                    "parameterName": name,
                },
                nil,
            ),
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

    /* the lock-free lookup, not Get: Get takes the read lock and sync.RWMutex is not reentrant, so calling it under the write lock would deadlock */
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
    parameter.name = name
    parameter.isSecret.Store(isSecret)

    /* the parameter is published before its template is resolved and removed again if the resolution fails, because the secret propagation marks the readers it finds in this map; every step runs under the write lock every reader takes, so no reader sees the intermediate state */
    instance.parameters[name] = parameter

    /* after the boot resolution a parameter is resolved on registration, or it would keep its raw template; a pre-resolve registration is left for the boot pass */
    if true == instance.resolved {
        stringValue, isString := value.(string)
        if true == isString {
            resolvedValue, resolveErr := instance.resolveTemplate(
                stringValue,
                name,
                make(map[string]bool),
                make(map[string]bool),
            )
            if nil != resolveErr {
                /* the rollback runs before the panic, or a half-made parameter would serve its raw template and burn the name for a corrected retry */
                delete(instance.parameters, name)

                exception.Panic(
                    exception.NewError(
                        "could not resolve a runtime parameter registered after boot",
                        exceptioncontract.Context{
                            "parameterName": name,
                        },
                        resolveErr,
                    ),
                )
            }

            parameter.storeValue(resolvedValue)
        }
    } else if stringValue, isString := value.(string); true == isString && true == templateCarriesConstruct(stringValue) {
        /* a pre-boot registration whose value carries a template construct is marked deferred, so a module reading it before the boot pass is refused instead of receiving the raw template; the boot pass clears the flag. The question is put to the resolution's grammar, so "Coverage 95%" is data. */
        parameter.deferred.Store(true)
    }
}

/* MarkSecret marks an already registered parameter as holding a credential, typically one melody registered from the .env artifacts before any module ran. An absent parameter is left alone, since an environment key may be undefined in some environments; the secret column of debug:parameters confirms a marking took effect. */
func (instance *Configuration) MarkSecret(name string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    parameter := instance.getInternalParameter(name)
    if nil == parameter {
        return false
    }

    parameter.isSecret.Store(true)

    /* the marking travels to every parameter whose template reads this one, under every spelling it answers to, whether it arrives before or after the boot resolve */
    instance.propagateSecretMarkLocked(name)

    return true
}

/* propagateSecretMarkLocked marks every parameter whose raw template reads the named one, through %env(NAME)%, the default processor's fallback or a %NAME% reference, under every spelling the name answers to, and follows the marking to a fixpoint. A match inside doubled-percent escaped text over-marks, which errs toward redacting more. */
func (instance *Configuration) propagateSecretMarkLocked(markedName string) {
    /* seeded with every spelling the marked parameter answers to, since a kernel-aliased parameter is one object under two names */
    markedNames := aliasesOfName(markedName)

    for 0 < len(markedNames) {
        currentName := markedNames[0]
        markedNames = markedNames[1:]

        for name, parameter := range instance.parameters {
            if true == parameter.isSecret.Load() {
                continue
            }

            templateValue, isString := parameter.environmentValue.(string)
            if false == isString {
                continue
            }

            if true == templateReadsName(templateValue, currentName) {
                parameter.isSecret.Store(true)
                markedNames = append(markedNames, aliasesOfName(name)...)
            }
        }
    }
}

func templateReadsName(template string, name string) bool {
    for _, submatches := range envPlaceholderPattern.FindAllStringSubmatch(template, -1) {
        if name == submatches[3] || name == submatches[2] {
            return true
        }
    }

    for _, submatches := range parameterPlaceholderPattern.FindAllStringSubmatch(template, -1) {
        if name == submatches[1] {
            return true
        }
    }

    return false
}

func (instance *Configuration) Names() []string {
    /* read under the read lock: RegisterRuntime mutates the parameters map at runtime */
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

/* EnvironmentKeyCount reports how many keys the .env artifacts contributed. Zero almost always means the files were not found, so the application can refuse to serve http on development defaults alone. */
func (instance *Configuration) EnvironmentKeyCount() int {
    return len(instance.environment.All())
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

    /* zero keys almost always means the .env artifacts were not found: melody derives the project directory from the executable location, the working directory under go run, so the directory searched is named */
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

/* the constructor's pass does not mark the configuration resolved: the composition root registers its parameters after it and before boot, and resolving those eagerly would make registration order significant. The pass defers a reference to a parameter not yet registered, and the boot pass resolves them all in one order-independent batch. */
func (instance *Configuration) resolvePlaceholders() error {
    resolveErr := instance.resolveAll(true)
    if nil != resolveErr {
        return exception.NewError("could not resolve the config parameters", nil, resolveErr)
    }

    instance.resolved = false

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

    staticExcludedPaths := splitHttpConfigurationList(instance.MustGet(KernelStaticExcludedPaths).MustString())

    sessionTtl, sessionTtlErr := instance.MustGet(KernelHttpSessionTtl).Duration()
    if nil != sessionTtlErr {
        return exception.NewError(
            "invalid environment value",
            exceptioncontract.Context{
                "environmentKey": HttpSessionTtlKey,
            },
            sessionTtlErr,
        )
    }

    sessionTombstoneRetention, sessionTombstoneRetentionErr := instance.MustGet(KernelHttpSessionTombstoneRetention).Duration()
    if nil != sessionTombstoneRetentionErr {
        return exception.NewError(
            "invalid environment value",
            exceptioncontract.Context{
                "environmentKey": HttpSessionTombstoneRetentionKey,
            },
            sessionTombstoneRetentionErr,
        )
    }

    shutdownTimeout, shutdownTimeoutErr := instance.MustGet(KernelHttpShutdownTimeout).Duration()
    if nil != shutdownTimeoutErr {
        return exception.NewError(
            "invalid environment value",
            exceptioncontract.Context{
                "environmentKey": HttpShutdownTimeoutKey,
            },
            shutdownTimeoutErr,
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
        staticExcludedPaths,
        sessionTtl,
        sessionTombstoneRetention,
        shutdownTimeout,
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

    /* walked in sorted order, so the boot fails on the same reserved-prefix key every run */
    environmentKeys := make([]string, 0, len(environment))
    for environmentKey := range environment {
        environmentKeys = append(environmentKeys, environmentKey)
    }
    sort.Strings(environmentKeys)

    for _, environmentKey := range environmentKeys {
        environmentValue := environment[environmentKey]

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

/* getInternalParameter is the lock-free map lookup; it must not take the lock, since it runs both at single-threaded construction and under the write lock. Concurrent readers go through Get, Parameters and Names. */
func (instance *Configuration) getInternalParameter(name string) *Parameter {
    parameter, exists := instance.parameters[name]
    if false == exists || nil == parameter {
        return nil
    }

    return parameter
}

var _ configcontract.Configuration = (*Configuration)(nil)
