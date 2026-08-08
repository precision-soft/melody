package config

import (
    "regexp"
    "sort"
    "strings"
    "sync"
    "sync/atomic"

    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

var (
    /* the optional "default:<fallback>:" prefix marks an environment key whose absence is tolerated: "%env(default::KEY)%" falls back to the empty string and "%env(default:some.parameter:KEY)%" falls back to another parameter. Without the prefix an undefined key stays a hard error, so a plain "%env(KEY)%" never silently degrades to empty. */
    envPlaceholderPattern = regexp.MustCompile(`%env\((default:([A-Za-z_][A-Za-z0-9_.]*)?:)?([A-Za-z_][A-Za-z0-9_]*)\)%`)
    /* a single-character name is a valid reference: the default processor's fallback group accepts one, so a reference pattern has to match it too or %a% silently survives as literal text */
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

    /* written and read under the configuration write lock so the refusal in Resolve is airtight — a MarkServing racing a Resolve either waits for the rewrite to finish (still pre-serving) or lands first and the rewrite is refused; the atomic wrapper keeps any future lock-free reader honest rather than carrying the synchronization itself */
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
    /* read under the read lock because RegisterRuntime mutates the shared parameters map at runtime; an unguarded range here races the writer and trips Go's fatal "concurrent map read and map write" */
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
    /* read under the read lock because RegisterRuntime mutates the shared parameters map at runtime; an unguarded read here races the writer and trips Go's fatal "concurrent map read and map write" */
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

    /* a name judged raw would let "  pool.size " register a parameter no lookup ever names: Get is an exact map lookup, so the padded spelling is unreachable through every accessor that types the name as written */
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

    /* use the lock-free lookup here, not Get: Get now takes the read lock and sync.RWMutex is non-reentrant, so self-calling it while holding the write lock would deadlock */
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

    /* the boot resolution has already run, so this parameter would otherwise keep its raw template — a %env(...)% reaching the consuming service verbatim. Resolve it now against the parameters boot left in place, and only then publish: resolving after the map write left a failed registration half-made, with the raw template served to every reader that outlived the recovered panic and the name burnt for any corrected retry. A pre-resolve registration is left raw for the boot pass to resolve in one batch. */
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
    }

    instance.parameters[name] = parameter
}

/* MarkSecret marks an already registered parameter as holding a credential. The parameters melody registers automatically from the .env artifacts are the ones most likely to hold one, and they exist before any module runs, so marking them is separate from declaring them.

An absent parameter is left alone rather than reported: an environment key is legitimately undefined in some environments, and refusing to boot over one would make the marking unusable exactly where it matters. The secret column of debug:parameters is what confirms a marking took effect. */
func (instance *Configuration) MarkSecret(name string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    parameter := instance.getInternalParameter(name)
    if nil == parameter {
        return false
    }

    parameter.isSecret.Store(true)

    /* the marking travels to every parameter whose template reads this one, exactly as it does when the marking precedes the resolution: without this, a MarkSecret arriving after the boot resolve redacted the key but left the dsn assembled from it printing in full, and the late marking reported success while covering half of what the early one covers. The scan reads the raw templates, so it reaches the same direct readers the resolution-time propagation reaches. */
    instance.propagateSecretMarkLocked(name)

    return true
}

/* propagateSecretMarkLocked marks every parameter whose raw template directly reads the named one — through %env(NAME)%, through the default processor's fallback, or through a %NAME% reference. A match inside doubled-percent escaped text over-marks, which errs toward redacting more, never less. Chained derivations (a template reading a template that reads the secret) are marked when their own direct source is marked; the transitive closure in one call is a v3 extension. */
func (instance *Configuration) propagateSecretMarkLocked(markedName string) {
    for _, parameter := range instance.parameters {
        if true == parameter.isSecret.Load() {
            continue
        }

        templateValue, isString := parameter.environmentValue.(string)
        if false == isString {
            continue
        }

        if true == templateReadsName(templateValue, markedName) {
            parameter.isSecret.Store(true)
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
    /* read under the read lock because RegisterRuntime mutates the shared parameters map at runtime; an unguarded range here races the writer and trips Go's fatal "concurrent map read and map write" */
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

/* EnvironmentKeyCount reports how many keys the .env artifacts contributed. Zero almost always means the files were not found rather than deliberately empty (the warning in applyEnvironmentOverrides names the same condition); the count is exposed so the application can refuse to serve http on nothing but development defaults instead of merely warning. */
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

/* the constructor's own pass does not mark the configuration resolved: the composition root registers its parameters after it and before boot, and a parameter registered while the configuration counts as resolved is resolved eagerly against whatever exists at that moment — which makes registration order significant and reports a forward reference as a failure "after boot" that boot has not yet reached. The pass is tolerant for the same reason: a .env value referencing a parameter the composition root registers next is deferred — unreadable until settled — rather than refused, and the boot pass resolves them all in one order-independent batch. */
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

/* getInternalParameter is the lock-free map lookup primitive; it must NOT take the lock because it is called both at single-threaded construction (placeholder resolution) and while the write lock is already held (RegisterRuntime). Concurrent readers go through Get/Parameters/Names, which take the read lock around it. */
func (instance *Configuration) getInternalParameter(name string) *Parameter {
    parameter, exists := instance.parameters[name]
    if false == exists || nil == parameter {
        return nil
    }

    return parameter
}

var _ configcontract.Configuration = (*Configuration)(nil)
