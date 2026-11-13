package config

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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

    if configuration.Kernel().LogsDirectory() != configuration.MustGet(KernelLogsDirectory).String() {
        t.Fatalf("expected LogsDirectory getter to match parameter value")
    }

    if configuration.Kernel().CacheDirectory() != configuration.MustGet(KernelCacheDirectory).String() {
        t.Fatalf("expected CacheDirectory getter to match parameter value")
    }
}

/* every kernel value is validated at construction, and each refusal names what it refused: these are the settings a deployment gets wrong in its .env, and the boot is the only place where saying so is still useful. A validator that stopped refusing would let the process start on a value nothing downstream can act on — a mode nothing dispatches, a role no runner matches, an environment that decides debug tooling. */
func TestNewKernelConfiguration_RefusesEveryValueItCannotAct(t *testing.T) {
    cases := []struct {
        name             string
        defaultMode      string
        processRole      string
        environment      string
        projectDirectory string
        logsDirectory    string
        logPath          string
        logLevel         string
        cacheDirectory   string
        expectedMessage  string
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
            name:             "an empty project directory",
            defaultMode:      ModeHttp,
            processRole:      RoleAll,
            environment:      EnvironmentDevelopment,
            projectDirectory: "",
            expectedMessage:  "project directory may not be empty",
        },
        {
            name:             "an empty logs directory",
            defaultMode:      ModeHttp,
            processRole:      RoleAll,
            environment:      EnvironmentDevelopment,
            projectDirectory: "/srv/app",
            logsDirectory:    "",
            expectedMessage:  "logs directory may not be empty",
        },
        {
            name:             "an empty cache directory",
            defaultMode:      ModeHttp,
            processRole:      RoleAll,
            environment:      EnvironmentDevelopment,
            projectDirectory: "/srv/app",
            logsDirectory:    "/srv/app/var/log",
            cacheDirectory:   "",
            expectedMessage:  "cache directory may not be empty",
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            logLevel := testCase.logLevel
            if "" == logLevel {
                logLevel = string(loggingcontract.LevelInfo)
            }

            kernelConfigurationInstance, kernelErr := newKernelConfiguration(testCase.defaultMode, testCase.processRole, testCase.environment, testCase.projectDirectory, testCase.logsDirectory, testCase.logPath, logLevel, testCase.cacheDirectory, bound.Duration{}, bound.Duration{}, bound.Count{})

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

/* the environment refusal names the key it is about, because the emptiness it refuses is read elsewhere and read differently: the dotenv source answers "dev" for a present-but-empty MELODY_ENV and loads .env.dev, so the process reaches this refusal having already taken the development values. Without the key an operator has nothing to search for, and without the sentence no reason the development files were the ones read. */
func TestNewKernelConfiguration_TheEmptyEnvironmentRefusalNamesTheKeyAndTheFiles(t *testing.T) {
    _, kernelErr := newKernelConfiguration(ModeHttp, RoleAll, "", "/srv/app", "/srv/app/var/log", "", string(loggingcontract.LevelInfo), "/srv/app/var/cache", bound.Duration{}, bound.Duration{}, bound.Count{})

    if nil == kernelErr {
        t.Fatalf("expected the empty environment to be refused")
    }

    var exceptionErr *exception.Error
    if false == errors.As(kernelErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got %v", kernelErr)
    }

    context := exceptionErr.Context()

    if EnvironmentNameKey != context["environmentKey"] {
        t.Fatalf("expected the refusal to name %q, got %#v", EnvironmentNameKey, context["environmentKey"])
    }

    if KernelEnvironment != context["parameterName"] {
        t.Fatalf("expected the refusal to name %q, got %#v", KernelEnvironment, context["parameterName"])
    }

    hint, ok := context["hint"].(string)
    if false == ok {
        t.Fatalf("expected the refusal to carry a hint, got %#v", context["hint"])
    }

    if false == strings.Contains(hint, ".env."+EnvironmentDevelopment) {
        t.Fatalf("expected the hint to name the files an empty value still selects, got %q", hint)
    }
}

/* a kernel configuration whose every value is acceptable is built and hands each one back, including the log path, which is allowed to be empty because an empty one means stdout */
func TestNewKernelConfiguration_AcceptsAValidSetAndKeepsEveryValue(t *testing.T) {
    kernelConfigurationInstance, kernelErr := newKernelConfiguration(ModeHttp, RoleWorker, EnvironmentProduction, "/srv/app", "/srv/app/var/log", "/srv/app/var/log/app.log", string(loggingcontract.LevelWarning), "/srv/app/var/cache", bound.Duration{}, bound.Duration{}, bound.Count{})
    if nil != kernelErr {
        t.Fatalf("unexpected error: %v", kernelErr)
    }

    if ModeHttp != kernelConfigurationInstance.DefaultMode() {
        t.Fatalf("unexpected default mode: %q", kernelConfigurationInstance.DefaultMode())
    }
    if RoleWorker != kernelConfigurationInstance.ProcessRole() {
        t.Fatalf("unexpected process role: %q", kernelConfigurationInstance.ProcessRole())
    }
    if EnvironmentProduction != kernelConfigurationInstance.Environment() {
        t.Fatalf("unexpected environment: %q", kernelConfigurationInstance.Environment())
    }
    if "/srv/app" != kernelConfigurationInstance.ProjectDirectory() {
        t.Fatalf("unexpected project directory: %q", kernelConfigurationInstance.ProjectDirectory())
    }
    if "/srv/app/var/log" != kernelConfigurationInstance.LogsDirectory() {
        t.Fatalf("unexpected logs directory: %q", kernelConfigurationInstance.LogsDirectory())
    }
    if "/srv/app/var/log/app.log" != kernelConfigurationInstance.LogPath() {
        t.Fatalf("unexpected log path: %q", kernelConfigurationInstance.LogPath())
    }
    if loggingcontract.LevelWarning != kernelConfigurationInstance.LogLevel() {
        t.Fatalf("unexpected log level: %q", kernelConfigurationInstance.LogLevel())
    }
    if "/srv/app/var/cache" != kernelConfigurationInstance.CacheDirectory() {
        t.Fatalf("unexpected cache directory: %q", kernelConfigurationInstance.CacheDirectory())
    }
}

/* the log level decides what every later record is filtered against, so a name the logger does not know is refused instead of falling back */
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
