package config

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func TestKernelGettersMatchParameterValues(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if configuration.Kernel().LogsDir() != configuration.MustGet(KernelLogsDir).String() {
        t.Fatalf("expected LogsDir getter to match parameter value")
    }

    if configuration.Kernel().CacheDir() != configuration.MustGet(KernelCacheDir).String() {
        t.Fatalf("expected CacheDir getter to match parameter value")
    }
}

func TestNewKernelConfiguration_RefusesEveryValueItCannotAct(t *testing.T) {
    cases := []struct {
        name            string
        defaultMode     string
        processRole     string
        environment     string
        projectDir      string
        logsDir         string
        logPath         string
        logLevel        string
        cacheDir        string
        expectedMessage string
    }{
        {
            name:            "an empty mode",
            defaultMode:     "",
            expectedMessage: "default mode may not be empty",
        },
        {
            name:            "a mode nothing dispatches",
            defaultMode:     "grpc",
            expectedMessage: "default mode is not supported",
        },
        {
            name:            "an empty process role",
            defaultMode:     ModeHttp,
            processRole:     "",
            expectedMessage: "process role may not be empty",
        },
        {
            name:            "a process role no runner matches",
            defaultMode:     ModeHttp,
            processRole:     "database",
            expectedMessage: "process role is not supported",
        },
        {
            name:            "an empty environment",
            defaultMode:     ModeHttp,
            processRole:     RoleAll,
            environment:     "",
            expectedMessage: "environment may not be empty",
        },
        {
            name:            "an environment nothing describes",
            defaultMode:     ModeHttp,
            processRole:     RoleAll,
            environment:     "staging",
            expectedMessage: "environment is not supported",
        },
        {
            name:            "an empty project directory",
            defaultMode:     ModeHttp,
            processRole:     RoleAll,
            environment:     EnvDevelopment,
            projectDir:      "",
            expectedMessage: "project directory may not be empty",
        },
        {
            name:            "an empty logs directory",
            defaultMode:     ModeHttp,
            processRole:     RoleAll,
            environment:     EnvDevelopment,
            projectDir:      "/srv/app",
            logsDir:         "",
            expectedMessage: "logs directory may not be empty",
        },
        {
            name:            "an empty cache directory",
            defaultMode:     ModeHttp,
            processRole:     RoleAll,
            environment:     EnvDevelopment,
            projectDir:      "/srv/app",
            logsDir:         "/srv/app/var/log",
            cacheDir:        "",
            expectedMessage: "cache directory may not be empty",
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            logLevel := testCase.logLevel
            if "" == logLevel {
                logLevel = string(loggingcontract.LevelInfo)
            }

            kernelConfigurationInstance, kernelErr := newKernelConfiguration(
                testCase.defaultMode,
                testCase.processRole,
                testCase.environment,
                testCase.projectDir,
                testCase.logsDir,
                testCase.logPath,
                logLevel,
                testCase.cacheDir,
            )

            if nil == kernelErr {
                t.Fatalf("expected the value to be refused")
            }

            if nil != kernelConfigurationInstance {
                t.Fatalf("expected no configuration over a refused value")
            }

            if testCase.expectedMessage != kernelErr.Error() {
                t.Fatalf("expected %q, got %q", testCase.expectedMessage, kernelErr.Error())
            }
        })
    }
}

func TestNewKernelConfiguration_TheEmptyEnvironmentRefusalNamesTheKeyAndTheFiles(t *testing.T) {
    _, kernelErr := newKernelConfiguration(
        ModeHttp,
        RoleAll,
        "",
        "/srv/app",
        "/srv/app/var/log",
        "",
        string(loggingcontract.LevelInfo),
        "/srv/app/var/cache",
    )

    if nil == kernelErr {
        t.Fatalf("expected the empty environment to be refused")
    }

    var exceptionErr *exception.Error
    if false == errors.As(kernelErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got %v", kernelErr)
    }

    context := exceptionErr.Context()

    if EnvKey != context["environmentKey"] {
        t.Fatalf("expected the refusal to name %q, got %#v", EnvKey, context["environmentKey"])
    }

    if KernelEnv != context["parameterName"] {
        t.Fatalf("expected the refusal to name %q, got %#v", KernelEnv, context["parameterName"])
    }

    hint, ok := context["hint"].(string)
    if false == ok {
        t.Fatalf("expected the refusal to carry a hint, got %#v", context["hint"])
    }

    if false == strings.Contains(hint, ".env."+EnvDevelopment) {
        t.Fatalf("expected the hint to name the files an empty value still selects, got %q", hint)
    }
}

func TestNewKernelConfiguration_AcceptsAValidSetAndKeepsEveryValue(t *testing.T) {
    kernelConfigurationInstance, kernelErr := newKernelConfiguration(
        ModeHttp,
        RoleWorker,
        EnvProduction,
        "/srv/app",
        "/srv/app/var/log",
        "/srv/app/var/log/app.log",
        string(loggingcontract.LevelWarning),
        "/srv/app/var/cache",
    )
    if nil != kernelErr {
        t.Fatalf("unexpected error: %v", kernelErr)
    }

    if ModeHttp != kernelConfigurationInstance.DefaultMode() {
        t.Fatalf("unexpected default mode: %q", kernelConfigurationInstance.DefaultMode())
    }
    if RoleWorker != kernelConfigurationInstance.ProcessRole() {
        t.Fatalf("unexpected process role: %q", kernelConfigurationInstance.ProcessRole())
    }
    if EnvProduction != kernelConfigurationInstance.Env() {
        t.Fatalf("unexpected environment: %q", kernelConfigurationInstance.Env())
    }
    if "/srv/app" != kernelConfigurationInstance.ProjectDir() {
        t.Fatalf("unexpected project directory: %q", kernelConfigurationInstance.ProjectDir())
    }
    if "/srv/app/var/log" != kernelConfigurationInstance.LogsDir() {
        t.Fatalf("unexpected logs directory: %q", kernelConfigurationInstance.LogsDir())
    }
    if "/srv/app/var/log/app.log" != kernelConfigurationInstance.LogPath() {
        t.Fatalf("unexpected log path: %q", kernelConfigurationInstance.LogPath())
    }
    if loggingcontract.LevelWarning != kernelConfigurationInstance.LogLevel() {
        t.Fatalf("unexpected log level: %q", kernelConfigurationInstance.LogLevel())
    }
    if "/srv/app/var/cache" != kernelConfigurationInstance.CacheDir() {
        t.Fatalf("unexpected cache directory: %q", kernelConfigurationInstance.CacheDir())
    }
}

func TestParseKernelLogLevel_MapsEveryKnownNameAndRefusesTheRest(t *testing.T) {
    known := []loggingcontract.Level{
        loggingcontract.LevelDebug,
        loggingcontract.LevelInfo,
        loggingcontract.LevelWarning,
        loggingcontract.LevelError,
        loggingcontract.LevelEmergency,
    }

    for _, level := range known {
        parsed, parseErr := parseKernelLogLevel(string(level))
        if nil != parseErr {
            t.Fatalf("unexpected error for %q: %v", level, parseErr)
        }
        if level != parsed {
            t.Fatalf("expected %q, got %q", level, parsed)
        }
    }

    _, emptyErr := parseKernelLogLevel("")
    if nil == emptyErr || "log level may not be empty" != emptyErr.Error() {
        t.Fatalf("expected the empty refusal, got %v", emptyErr)
    }

    unknownLevel, unknownErr := parseKernelLogLevel("verbose")
    if nil == unknownErr || "log level is not supported" != unknownErr.Error() {
        t.Fatalf("expected the unknown refusal, got %v", unknownErr)
    }
    if loggingcontract.LevelUnknown != unknownLevel {
        t.Fatalf("expected the unknown level, got %q", unknownLevel)
    }
}
