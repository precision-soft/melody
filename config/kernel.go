package config

import (
    configcontract "github.com/precision-soft/melody/config/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

func newKernelConfiguration(
    defaultMode string,
    environment string,
    projectDir string,
    logsDir string,
    logPath string,
    logLevel string,
    cacheDir string,
) (*kernelConfiguration, error) {
    parsedLogLevel, parseLogLevelErr := parseKernelLogLevel(logLevel)
    if nil != parseLogLevelErr {
        return nil, parseLogLevelErr
    }

    kernelConfigurationInstance := &kernelConfiguration{
        defaultMode: defaultMode,
        env:         environment,
        projectDir:  projectDir,
        logsDir:     logsDir,
        logPath:     logPath,
        logLevel:    parsedLogLevel,
        cacheDir:    cacheDir,
    }

    validateErr := kernelConfigurationInstance.validate()
    if nil != validateErr {
        return nil, validateErr
    }

    return kernelConfigurationInstance, nil
}

type kernelConfiguration struct {
    defaultMode string
    env         string
    projectDir  string
    logsDir     string
    logPath     string
    logLevel    loggingcontract.Level
    cacheDir    string
}

func (instance *kernelConfiguration) DefaultMode() string {
    return instance.defaultMode
}

func (instance *kernelConfiguration) Env() string {
    return instance.env
}

func (instance *kernelConfiguration) ProjectDir() string {
    return instance.projectDir
}

func (instance *kernelConfiguration) LogsDir() string {
    return instance.logsDir
}

func (instance *kernelConfiguration) LogPath() string {
    return instance.logPath
}

func (instance *kernelConfiguration) LogLevel() loggingcontract.Level {
    return instance.logLevel
}

func (instance *kernelConfiguration) CacheDir() string {
    return instance.cacheDir
}

func (instance *kernelConfiguration) validate() error {
    validateDefaultModeErr := instance.validateDefaultMode()
    if nil != validateDefaultModeErr {
        return validateDefaultModeErr
    }

    validateEnvironmentErr := instance.validateEnvironment()
    if nil != validateEnvironmentErr {
        return validateEnvironmentErr
    }

    validateProjectDirErr := instance.validateProjectDir()
    if nil != validateProjectDirErr {
        return validateProjectDirErr
    }

    validateLogsDirErr := instance.validateLogsDir()
    if nil != validateLogsDirErr {
        return validateLogsDirErr
    }

    validateLogPathErr := instance.validateLogPath()
    if nil != validateLogPathErr {
        return validateLogPathErr
    }

    validateCacheDirErr := instance.validateCacheDir()
    if nil != validateCacheDirErr {
        return validateCacheDirErr
    }

    return nil
}

func (instance *kernelConfiguration) validateDefaultMode() error {
    defaultMode := instance.DefaultMode()
    if "" == defaultMode {
        return exception.NewError("default mode may not be empty", nil, nil)
    }

    switch defaultMode {
    case ModeHttp, ModeCli:
        return nil
    }

    return exception.NewError(
        "default mode is not supported",
        exceptioncontract.Context{
            "mode": defaultMode,
        },
        nil,
    )
}

func (instance *kernelConfiguration) validateEnvironment() error {
    environment := instance.Env()
    if "" == environment {
        return exception.NewError("environment may not be empty", nil, nil)
    }

    switch environment {
    case EnvDevelopment, EnvProduction:
        return nil
    }

    return exception.NewError(
        "environment is not supported",
        exceptioncontract.Context{
            "environment": environment,
        },
        nil,
    )
}

func (instance *kernelConfiguration) validateProjectDir() error {
    if "" == instance.projectDir {
        return exception.NewError("project directory may not be empty", nil, nil)
    }

    return nil
}

func (instance *kernelConfiguration) validateLogsDir() error {
    if "" == instance.logsDir {
        return exception.NewError("logs directory may not be empty", nil, nil)
    }

    return nil
}

func (instance *kernelConfiguration) validateLogPath() error {
    logPath := instance.logPath

    if "" == logPath {
        return nil
    }

    if true == envPlaceholderPattern.MatchString(logPath) || true == parameterPlaceholderPattern.MatchString(logPath) {
        return exception.NewError(
            "log path contains unresolved placeholders",
            exceptioncontract.Context{
                "logPath": logPath,
            },
            nil,
        )
    }

    return nil
}

func (instance *kernelConfiguration) validateCacheDir() error {
    if "" == instance.cacheDir {
        return exception.NewError("cache directory may not be empty", nil, nil)
    }

    return nil
}

var _ configcontract.KernelConfiguration = (*kernelConfiguration)(nil)

func parseKernelLogLevel(logLevel string) (loggingcontract.Level, error) {
    if "" == logLevel {
        return loggingcontract.LevelUnknown, exception.NewError(
            "log level may not be empty",
            nil,
            nil,
        )
    }

    switch logLevel {
    case string(loggingcontract.LevelDebug):
        return loggingcontract.LevelDebug, nil
    case string(loggingcontract.LevelInfo):
        return loggingcontract.LevelInfo, nil
    case string(loggingcontract.LevelWarning):
        return loggingcontract.LevelWarning, nil
    case string(loggingcontract.LevelError):
        return loggingcontract.LevelError, nil
    case string(loggingcontract.LevelEmergency):
        return loggingcontract.LevelEmergency, nil
    }

    return loggingcontract.LevelUnknown, exception.NewError(
        "log level is not supported",
        exceptioncontract.Context{
            "logLevel": logLevel,
        },
        nil,
    )
}
