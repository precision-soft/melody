package config

import (
    "regexp"
    "slices"
    "sort"
    "strings"
    "sync"
    "sync/atomic"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

var (
    /* a dot is admitted, so a dotted .env line, no parameter now, stays readable */
    envPlaceholderPattern = regexp.MustCompile(`%env\(([A-Za-z_][A-Za-z0-9_.]*)\)%`)
)

/* ConfigurationConfig is what a configuration is built from beside its environment and project directory; a field left out is empty. */
type ConfigurationConfig struct {
    /* EnvironmentKeys are the application's declared keys, entered into the key table after the framework's and before the first resolve, owned by the application. */
    EnvironmentKeys []configcontract.EnvironmentKey

    /* ParameterOverrides are the application's parameter overrides, by parameter name: a kernel.* one applied at construction, any other held for the module that declares the key feeding it. */
    ParameterOverrides map[string]string

    /* HttpPublicUrl is the application's public base, ApplicationConfig.HttpPublicUrl: HttpConfiguration.PublicUrl answers it when MELODY_HTTP_PUBLIC_URL is unset, judged as the key is and refused naming the field. */
    HttpPublicUrl string
}

func NewConfiguration(
    environment *Environment,
    projectDirectory string,
    configurationConfig ConfigurationConfig,
) (*Configuration, error) {
    if nil == environment {
        return nil, exception.NewError("environment is required", nil, nil)
    }

    logger := logging.EmergencyLogger()

    configuration := &Configuration{
        environment:   environment,
        parameters:    make(ParameterMap),
        logger:        logger,
        httpPublicUrl: configurationConfig.HttpPublicUrl,
    }

    applyDefaultsErr := configuration.applyDefaults(projectDirectory)
    if nil != applyDefaultsErr {
        return nil, applyDefaultsErr
    }

    applyEnvironmentOverridesErr := configuration.applyEnvironmentOverrides()
    if nil != applyEnvironmentOverridesErr {
        return nil, applyEnvironmentOverridesErr
    }

    declareApplicationEnvironmentKeysErr := configuration.declareApplicationEnvironmentKeys(configurationConfig.EnvironmentKeys)
    if nil != declareApplicationEnvironmentKeysErr {
        return nil, declareApplicationEnvironmentKeysErr
    }

    applyParameterOverridesErr := configuration.applyParameterOverrides(configurationConfig.ParameterOverrides)
    if nil != applyParameterOverridesErr {
        return nil, applyParameterOverridesErr
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

/* Configuration holds the environment, the parameters and the cli, kernel and http sections; after MarkServing a Resolve is refused. */
type Configuration struct {
    mutex       sync.RWMutex
    environment *Environment
    parameters  ParameterMap
    logger      loggingcontract.Logger
    cli         *cliConfiguration
    kernel      *kernelConfiguration
    http        *httpConfiguration

    /* the application's public base, read when the key is unset */
    httpPublicUrl string

    resolved bool

    /* written and read under the write lock; the atomic is for lock-free readers, not the synchronization */
    serving atomic.Bool

    /* the framework's keys and every declared one */
    declaredEnvironmentKeys       map[string]declaredEnvironmentKey
    declaredEnvironmentParameters map[string]string

    secretEnvironmentKeys map[string]bool

    /* keyed by the retired spelling, holding the declared key that replaced it */
    declaredRetiredEnvironmentKeys map[string]string

    /* overrides of a parameter no key declared at construction feeds, by parameter name, consumed by the declaration of its key */
    pendingParameterOverrides map[string]string
}

/* MarkServing ends the wiring phase: Resolve is refused from then on, and a parameter registered later resolves on registration. */
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
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return internal.CopyStringMap[*Parameter](
        instance.parameters,
    )
}

func (instance *Configuration) projectDirectoryParameterValue() string {
    parameter := instance.Get(KernelProjectDirectory)
    if true == internal.IsNilInterface(parameter) {
        return ""
    }

    return parameter.String()
}

func (instance *Configuration) Get(name string) configcontract.Parameter {
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
    if true == internal.IsNilInterface(parameter) {
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

/* RegisterRuntimeSecret registers a parameter holding a credential, redacted by the commands that render the configuration; the marking reaches the parameters whose template reads it, not the one it reads. */
func (instance *Configuration) RegisterRuntimeSecret(name string, value any) {
    instance.registerRuntimeParameter(name, value, true)
}

func (instance *Configuration) registerRuntimeParameter(name string, value any, isSecret bool) {
    if "" == strings.TrimSpace(name) {
        exception.Panic(
            exception.NewError("cannot register parameters with empty names", nil, nil),
        )
    }

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

    /* not Get: Get takes the read lock and sync.RWMutex is not reentrant */
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

    /* published before the resolution, since the secret propagation reads this map, and removed again on failure */
    instance.parameters[name] = parameter

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
        /* deferred so a module reading it before the boot pass is refused rather than handed the raw template */
        parameter.deferred.Store(true)
    }
}

/* MarkSecret marks a registered parameter as holding a credential; an absent parameter is left alone. */
func (instance *Configuration) MarkSecret(name string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    parameter := instance.getInternalParameter(name)
    if nil == parameter {
        return false
    }

    parameter.isSecret.Store(true)

    instance.propagateSecretMarkLocked(name)

    return true
}

/* a mark travels over two tables: a parameter to the parameters and keys reading it, a key to the parameters and keys reading it and the keys a .env value assembled from it; a match inside doubled-percent escaped text over-marks, which errs toward redacting more */
func (instance *Configuration) propagateSecretMarkLocked(markedName string) {
    instance.propagateSecretMarksLocked([]string{markedName}, nil)
}

func (instance *Configuration) propagateEnvironmentKeySecretMarkLocked(markedKey string) {
    instance.markEnvironmentKeySecretLocked(markedKey)
    instance.propagateSecretMarksLocked(nil, []string{markedKey})
}

func (instance *Configuration) markEnvironmentKeySecretLocked(key string) bool {
    if true == instance.secretEnvironmentKeys[key] {
        return false
    }

    if nil == instance.secretEnvironmentKeys {
        instance.secretEnvironmentKeys = make(map[string]bool)
    }

    instance.secretEnvironmentKeys[key] = true

    return true
}

func (instance *Configuration) propagateSecretMarksLocked(markedParameters []string, markedKeys []string) {
    for 0 < len(markedParameters) || 0 < len(markedKeys) {
        if 0 < len(markedParameters) {
            currentName := markedParameters[0]
            markedParameters = markedParameters[1:]

            /* the parameter a declared key feeds, not overridden, holds the key's value, so the key's other readers carry it too */
            if feedingKey, isFed := instance.declaredEnvironmentParameters[currentName]; true == isFed && false == instance.isOverriddenLocked(currentName) {
                if true == instance.markEnvironmentKeySecretLocked(feedingKey) {
                    markedKeys = append(markedKeys, feedingKey)
                }
            }

            markedParameters = append(markedParameters, instance.markParametersReadingLocked(currentName, false)...)

            for _, readingKey := range instance.environmentKeysReadingLocked(currentName, false) {
                if true == instance.markEnvironmentKeySecretLocked(readingKey) {
                    markedKeys = append(markedKeys, readingKey)
                }
            }

            continue
        }

        currentKey := markedKeys[0]
        markedKeys = markedKeys[1:]

        markedParameters = append(markedParameters, instance.markParametersReadingLocked(currentKey, true)...)

        readingKeys := instance.environmentKeysReadingLocked(currentKey, true)

        /* a .env value assembled from ${NAME} carries NAME's credential although no template names it */
        readingKeys = append(readingKeys, instance.environment.keysAssembledFrom(currentKey)...)

        for _, readingKey := range readingKeys {
            if true == instance.markEnvironmentKeySecretLocked(readingKey) {
                markedKeys = append(markedKeys, readingKey)
            }
        }
    }
}

func (instance *Configuration) markParametersReadingLocked(name string, readsKey bool) []string {
    markedNames := make([]string, 0)

    for parameterName, parameter := range instance.parameters {
        if true == parameter.isSecret.Load() {
            continue
        }

        templateValue, isString := parameter.template.(string)
        if false == isString {
            continue
        }

        if true == templateReads(templateValue, name, readsKey) {
            parameter.isSecret.Store(true)
            markedNames = append(markedNames, parameterName)
        }
    }

    return markedNames
}

/* %env(KEY)% reads a key's line or declared default, so those texts are templates too */
func (instance *Configuration) environmentKeysReadingLocked(name string, readsKey bool) []string {
    candidates := make(map[string]bool)
    if nil != instance.environment {
        for key := range instance.environment.values {
            candidates[key] = true
        }
    }

    for key := range instance.declaredEnvironmentKeys {
        candidates[key] = true
    }

    readingKeys := make([]string, 0)
    for key := range candidates {
        if true == readsKey && key == name {
            continue
        }

        text, isKnown := instance.environmentKeyText(key, true)
        if true == isKnown && true == templateReads(text, name, readsKey) {
            readingKeys = append(readingKeys, key)
        }
    }

    sort.Strings(readingKeys)

    return readingKeys
}

/* a doubled percent is a literal: "a%%%db.pass%" reads db.pass and "a%%db.pass%%" reads nothing */
func templateReferences(template string) ([]string, []string) {
    keys := make([]string, 0)
    names := make([]string, 0)

    index := 0
    for index < len(template) {
        if '%' != template[index] {
            index = index + 1

            continue
        }

        if index+1 < len(template) && '%' == template[index+1] {
            index = index + 2

            continue
        }

        fragment := template[index:]

        if true == strings.HasPrefix(fragment, "%env(") {
            innerEnd := len("%env(")
            for innerEnd < len(fragment) && '%' != fragment[innerEnd] {
                if ')' == fragment[innerEnd] && innerEnd+1 < len(fragment) && '%' == fragment[innerEnd+1] {
                    break
                }

                innerEnd = innerEnd + 1
            }

            if innerEnd < len(fragment) && '%' != fragment[innerEnd] {
                candidate := fragment[:innerEnd+2]

                submatches := envPlaceholderPattern.FindStringSubmatch(candidate)
                if nil != submatches && candidate == submatches[0] {
                    keys = append(keys, submatches[1])

                    index = index + len(candidate)

                    continue
                }
            }

            index = index + 1

            continue
        }

        parameterKey, consumedLength, _ := parseParameterPlaceholder(fragment)
        if 0 < consumedLength {
            names = append(names, parameterKey)
            index = index + consumedLength

            continue
        }

        index = index + 1
    }

    return keys, names
}

func templateReads(template string, name string, readsKey bool) bool {
    keys, names := templateReferences(template)
    if true == readsKey {
        return slices.Contains(keys, name)
    }

    return slices.Contains(names, name)
}

func (instance *Configuration) Names() []string {
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

/* EnvironmentKeyCount reports how many keys the .env artifacts contributed; zero almost always means the files were not found. */
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

/* the constructor's pass leaves the configuration unresolved, so registration order stays insignificant */
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
    bootWarningsDisabled, bootWarningsDisabledErr := checkedParameterSwitch(instance.MustGet(KernelCliBootWarningsDisabled), CliBootWarningsDisabledKey)
    if nil != bootWarningsDisabledErr {
        return bootWarningsDisabledErr
    }

    cliConfigurationInstance, newCliConfigurationErr := newCliConfiguration(
        instance.MustGet(KernelCliName).MustString(),
        instance.MustGet(KernelCliDescription).MustString(),
        splitConfigurationList(instance.MustGet(KernelCliBootWarningsIgnored).MustString()),
        bootWarningsDisabled.Value,
    )
    if nil != newCliConfigurationErr {
        return exception.NewError("could not initialize the cli configuration", nil, newCliConfigurationErr)
    }

    instance.cli = cliConfigurationInstance

    instance.logger.Info("configuration cli initialized", nil)

    return nil
}

func (instance *Configuration) buildKernelConfiguration() error {
    teardownTimeout, teardownTimeoutErr := checkedParameterLimit(instance.MustGet(KernelTeardownTimeout), TeardownTimeoutKey, TeardownTimeoutRule())
    if nil != teardownTimeoutErr {
        return teardownTimeoutErr
    }

    scopeTeardownTimeout, scopeTeardownTimeoutErr := checkedParameterLimit(instance.MustGet(KernelScopeTeardownTimeout), ScopeTeardownTimeoutKey, container.ScopeTeardownTimeoutRule())
    if nil != scopeTeardownTimeoutErr {
        return scopeTeardownTimeoutErr
    }

    containerCreationWaiters, containerCreationWaitersErr := checkedParameterLimit(instance.MustGet(KernelContainerCreationWaiters), ContainerCreationWaitersKey, container.CreationWaitersRule())
    if nil != containerCreationWaitersErr {
        return containerCreationWaitersErr
    }

    containerTeardownSequential, containerTeardownSequentialErr := checkedParameterSwitch(instance.MustGet(KernelContainerTeardownSequential), ContainerTeardownSequentialKey)
    if nil != containerTeardownSequentialErr {
        return containerTeardownSequentialErr
    }

    kernelConfigurationInstance, newKernelConfigurationErr := newKernelConfiguration(
        instance.MustGet(KernelDefaultMode).MustString(),
        instance.MustGet(KernelProcessRole).MustString(),
        instance.MustGet(KernelEnvironment).MustString(),
        instance.MustGet(KernelProjectDirectory).MustString(),
        instance.MustGet(KernelLogsDirectory).MustString(),
        instance.MustGet(KernelLogPath).MustString(),
        instance.MustGet(KernelLogLevel).MustString(),
        instance.MustGet(KernelCacheDirectory).MustString(),
        teardownTimeout,
        scopeTeardownTimeout,
        containerCreationWaiters,
    )
    if nil != newKernelConfigurationErr {
        return exception.NewError("could not initialize the kernel configuration", nil, newKernelConfigurationErr)
    }

    kernelConfigurationInstance.containerTeardownSequential = containerTeardownSequential

    instance.kernel = kernelConfigurationInstance

    instance.logger.Info("configuration kernel initialized", nil)

    return nil
}

func (instance *Configuration) buildHttpConfiguration() error {
    httpMaxRequestBodyBytes, httpMaxRequestBodyBytesErr := resolveParameter(instance.MustGet(KernelHttpMaxRequestBodyBytes), HttpMaxRequestBodyBytesKey, MaxRequestBodyBytesRule())
    if nil != httpMaxRequestBodyBytesErr {
        return httpMaxRequestBodyBytesErr
    }

    staticCacheMaxAge, staticCacheMaxAgeErr := resolveParameter(instance.MustGet(KernelStaticCacheMaxAge), StaticCacheMaxAgeKey, StaticCacheMaxAgeRule())
    if nil != staticCacheMaxAgeErr {
        return staticCacheMaxAgeErr
    }

    staticCacheDisabledSwitch, staticCacheDisabledErr := checkedParameterSwitch(instance.MustGet(KernelStaticCacheDisabled), StaticCacheDisabledKey)
    if nil != staticCacheDisabledErr {
        return staticCacheDisabledErr
    }

    /* an empty line is unset and reads false, the cache on */
    staticCacheDisabled := staticCacheDisabledSwitch.Value

    staticExcludedPaths := splitConfigurationList(instance.MustGet(KernelStaticExcludedPaths).MustString())

    sessionTtl, sessionTtlErr := resolveParameter(instance.MustGet(KernelHttpSessionTtl), HttpSessionTtlKey, SessionTtlRule())
    if nil != sessionTtlErr {
        return sessionTtlErr
    }

    sessionTombstoneRetention, sessionTombstoneRetentionErr := resolveParameter(instance.MustGet(KernelHttpSessionTombstoneRetention), HttpSessionTombstoneRetentionKey, SessionTombstoneRetentionRule())
    if nil != sessionTombstoneRetentionErr {
        return sessionTombstoneRetentionErr
    }

    sessionStorageTimeout, sessionStorageTimeoutErr := resolveParameter(instance.MustGet(KernelHttpSessionStorageTimeout), HttpSessionStorageTimeoutKey, SessionStorageTimeoutRule())
    if nil != sessionStorageTimeoutErr {
        return sessionStorageTimeoutErr
    }

    sessionMaxSessions, sessionMaxSessionsErr := resolveParameter(instance.MustGet(KernelHttpSessionMaxSessions), HttpSessionMaxSessionsKey, SessionMaxSessionsRule())
    if nil != sessionMaxSessionsErr {
        return sessionMaxSessionsErr
    }

    shutdownTimeout, shutdownTimeoutErr := checkedParameterLimit(instance.MustGet(KernelHttpShutdownTimeout), HttpShutdownTimeoutKey, ShutdownTimeoutRule())
    if nil != shutdownTimeoutErr {
        return shutdownTimeoutErr
    }

    serverLimits := httpServerLimits{}

    var serverLimitErr error
    if serverLimits.readTimeout, serverLimitErr = checkedParameterLimit(instance.MustGet(KernelHttpReadTimeout), HttpReadTimeoutKey, HttpReadTimeoutRule()); nil != serverLimitErr {
        return serverLimitErr
    }

    if serverLimits.readHeaderTimeout, serverLimitErr = checkedParameterLimit(instance.MustGet(KernelHttpReadHeaderTimeout), HttpReadHeaderTimeoutKey, HttpReadHeaderTimeoutRule()); nil != serverLimitErr {
        return serverLimitErr
    }

    if serverLimits.writeTimeout, serverLimitErr = checkedParameterLimit(instance.MustGet(KernelHttpWriteTimeout), HttpWriteTimeoutKey, HttpWriteTimeoutRule()); nil != serverLimitErr {
        return serverLimitErr
    }

    if serverLimits.idleTimeout, serverLimitErr = checkedParameterLimit(instance.MustGet(KernelHttpIdleTimeout), HttpIdleTimeoutKey, HttpIdleTimeoutRule()); nil != serverLimitErr {
        return serverLimitErr
    }

    if serverLimits.maxHeaderBytes, serverLimitErr = checkedParameterLimit(instance.MustGet(KernelHttpMaxHeaderBytes), HttpMaxHeaderBytesKey, HttpMaxHeaderBytesRule()); nil != serverLimitErr {
        return serverLimitErr
    }

    if serverLimits.unavailableRetryAfter, serverLimitErr = checkedUnavailableRetryAfter(instance.MustGet(KernelHttpUnavailableRetryAfter)); nil != serverLimitErr {
        return serverLimitErr
    }

    if serverLimits.sessionEmptyOnOutage, serverLimitErr = checkedParameterSwitch(instance.MustGet(KernelHttpSessionEmptyOnOutage), HttpSessionEmptyOnOutageKey); nil != serverLimitErr {
        return serverLimitErr
    }

    publicUrl := instance.MustGet(KernelHttpPublicUrl).MustString()
    if "" == publicUrl {
        normalizedPublicUrl, publicUrlRefusal := internal.NormalizePublicUrl(instance.httpPublicUrl)
        if "" != publicUrlRefusal {
            return exception.NewError(
                "the public url is refused",
                exceptioncontract.Context{"field": "ApplicationConfig.HttpPublicUrl", "key": HttpPublicUrlKey, "value": instance.httpPublicUrl, "reason": publicUrlRefusal},
                nil,
            )
        }

        publicUrl = normalizedPublicUrl
    }

    httpConfigurationInstance, newHttpConfigurationErr := newHttpConfiguration(
        instance.MustGet(KernelHttpAddress).MustString(),
        publicUrl,
        instance.MustGet(KernelDefaultLocale).MustString(),
        instance.MustGet(KernelPublicDirectory).MustString(),
        instance.MustGet(KernelStaticIndexFile).MustString(),
        httpMaxRequestBodyBytes,
        staticCacheDisabled,
        staticCacheMaxAge,
        staticExcludedPaths,
        sessionTtl,
        sessionTombstoneRetention,
        sessionStorageTimeout,
        sessionMaxSessions,
        shutdownTimeout,
        serverLimits,
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

    /* sorted so the boot fails on the same reserved-prefix key every run */
    environmentKeys := make([]string, 0, len(environment))
    for environmentKey := range environment {
        environmentKeys = append(environmentKeys, environmentKey)
    }
    sort.Strings(environmentKeys)

    for _, environmentKey := range environmentKeys {
        if true == instance.isReserved(environmentKey) {
            return exception.NewError(
                "environment key uses reserved parameter prefix",
                exceptioncontract.Context{
                    "environmentKey": environmentKey,
                },
                nil,
            )
        }
    }

    return nil
}

func (instance *Configuration) isReserved(name string) bool {
    return strings.HasPrefix(name, "kernel.")
}

/* lock-free: it runs at construction and under the write lock */
func (instance *Configuration) getInternalParameter(name string) *Parameter {
    parameter, exists := instance.parameters[name]
    if false == exists || nil == parameter {
        return nil
    }

    return parameter
}

var _ configcontract.Configuration = (*Configuration)(nil)
