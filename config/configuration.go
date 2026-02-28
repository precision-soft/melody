package config

import (
    "regexp"
    "sort"
    "strings"

    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

var (
    envPlaceholderPattern       = regexp.MustCompile(`%env\(([A-Za-z0-9_]+)\)%`)
    parameterPlaceholderPattern = regexp.MustCompile(`%([A-Za-z0-9_.]+)%`)
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
    environment *Environment
    parameters  ParameterMap
    logger      loggingcontract.Logger
    cli         *cliConfiguration
    kernel      *kernelConfiguration
    http        *httpConfiguration
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
    return internal.CopyStringMap[*Parameter](
        instance.parameters,
    )
}

func (instance *Configuration) Get(name string) configcontract.Parameter {
    parameter, exists := instance.parameters[name]
    if false == exists || nil == parameter {
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

    existingParameter := instance.Get(name)
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

    instance.parameters[name] = NewParameter("", value, value, false)
}

func (instance *Configuration) Names() []string {
    names := make([]string, 0, len(instance.parameters))

    for name := range instance.parameters {
        names = append(names, name)
    }

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

func (instance *Configuration) getInternalParameter(name string) *Parameter {
    parameter, exists := instance.parameters[name]
    if false == exists || nil == parameter {
        return nil
    }

    return parameter
}

var _ configcontract.Configuration = (*Configuration)(nil)
