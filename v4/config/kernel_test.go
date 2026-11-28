package config

import (
    "context"
    "errors"
    "os"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func TestKernelGettersMatchParameterValues(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(context.Background(), source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody", ConfigurationConfig{})
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

func TestKernelConfiguration_TheModeKeysDefaultTo0640And0750(t *testing.T) {
    kernel := mustConfigurationFromValues(t, map[string]string{}).Kernel()

    if os.FileMode(0o640) != kernel.LogFileMode() || os.FileMode(0o750) != kernel.LogDirectoryMode() {
        t.Fatalf("expected 0640 and 0750, got %o and %o", kernel.LogFileMode(), kernel.LogDirectoryMode())
    }
}

func TestKernelConfiguration_TheCacheDirectoryModeKey(t *testing.T) {
    if os.FileMode(0o750) != DefaultCacheDirectoryMode || os.FileMode(0o750) != mustConfigurationFromValues(t, map[string]string{}).Kernel().CacheDirectoryMode() {
        t.Fatalf("expected the cache directory at 0750 by default, got %o", DefaultCacheDirectoryMode)
    }

    for text, expected := range map[string]os.FileMode{"0755": 0o755, "700": 0o700, " 0o770 ": 0o770} {
        if actual := mustConfigurationFromValues(t, map[string]string{CacheDirectoryModeKey: text}).Kernel().CacheDirectoryMode(); expected != actual {
            t.Fatalf("%q: expected %o, got %o", text, expected, actual)
        }
    }

    for _, refused := range []string{"0x1ed", "02750", "0600", "0650", ""} {
        _, err := newConfigurationFromValues(map[string]string{CacheDirectoryModeKey: refused})
        assertLimitRefusal(t, err, CacheDirectoryModeKey, "")

        found := false
        for current := err; nil != current; current = errors.Unwrap(current) {
            refusal, isRefusal := current.(*exception.Error)
            if true == isRefusal && internal.DirectoryModeRefusal == refusal.Message() && refused == refusal.Context()["value"] {
                found = true
            }
        }

        if false == found {
            t.Fatalf("%q: expected the directory mode refusal carrying the value, got %v", refused, err)
        }
    }
}

func TestKernelConfiguration_TheModeKeysAreReadAsOctal(t *testing.T) {
    for _, testCase := range []struct {
        fileText      string
        directoryText string
        fileMode      os.FileMode
        directoryMode os.FileMode
    }{
        {fileText: "0644", directoryText: "0755", fileMode: 0o644, directoryMode: 0o755},
        {fileText: "600", directoryText: "700", fileMode: 0o600, directoryMode: 0o700},
        {fileText: " 0o660 ", directoryText: "0o770", fileMode: 0o660, directoryMode: 0o770},
    } {
        kernel := mustConfigurationFromValues(t, map[string]string{LogFileModeKey: testCase.fileText, LogDirectoryModeKey: testCase.directoryText}).Kernel()

        if testCase.fileMode != kernel.LogFileMode() || testCase.directoryMode != kernel.LogDirectoryMode() {
            t.Fatalf("%q, %q: expected %o and %o, got %o and %o", testCase.fileText, testCase.directoryText, testCase.fileMode, testCase.directoryMode, kernel.LogFileMode(), kernel.LogDirectoryMode())
        }
    }
}

func TestKernelConfiguration_TheModeKeysRule(t *testing.T) {
    for _, testCase := range []struct {
        key     string
        value   string
        message string
    }{
        {key: LogFileModeKey, value: "rw-r-----", message: internal.JournalFileModeRefusal},
        {key: LogFileModeKey, value: "0648", message: internal.JournalFileModeRefusal},
        {key: LogFileModeKey, value: "01640", message: internal.JournalFileModeRefusal},
        {key: LogFileModeKey, value: "0400", message: internal.JournalFileModeRefusal},
        {key: LogFileModeKey, value: "0200", message: internal.JournalFileModeRefusal},
        {key: LogFileModeKey, value: "", message: internal.JournalFileModeRefusal},
        {key: LogFileModeKey, value: "  ", message: internal.JournalFileModeRefusal},
        {key: LogDirectoryModeKey, value: "0x1ed", message: internal.JournalDirectoryModeRefusal},
        {key: LogDirectoryModeKey, value: "02750", message: internal.JournalDirectoryModeRefusal},
        {key: LogDirectoryModeKey, value: "0600", message: internal.JournalDirectoryModeRefusal},
        {key: LogDirectoryModeKey, value: "0650", message: internal.JournalDirectoryModeRefusal},
        {key: LogDirectoryModeKey, value: "", message: internal.JournalDirectoryModeRefusal},
    } {
        _, err := newConfigurationFromValues(map[string]string{testCase.key: testCase.value})
        assertLimitRefusal(t, err, testCase.key, "")

        found := false
        for current := err; nil != current; current = errors.Unwrap(current) {
            refusal, isRefusal := current.(*exception.Error)
            if true == isRefusal && testCase.message == refusal.Message() && testCase.value == refusal.Context()["value"] {
                found = true
            }
        }

        if false == found {
            t.Fatalf("%s=%q: expected the refusal %q carrying the value, got %v", testCase.key, testCase.value, testCase.message, err)
        }
    }
}

func TestConfigurationBootWarningsIgnoredIsReadAndDefaultsToNothing(t *testing.T) {
    defaulted := mustConfigurationFromValues(t, map[string]string{}).Kernel()
    if nil == defaulted.BootWarningsIgnored() || 0 != len(defaulted.BootWarningsIgnored()) {
        t.Fatalf("expected no ignored code, got %v", defaulted.BootWarningsIgnored())
    }

    ignored := mustConfigurationFromValues(t, map[string]string{BootWarningsIgnoredKey: " " + internal.BootWarningNameFirewallDoorReachUnknown + " "}).Kernel()
    if 1 != len(ignored.BootWarningsIgnored()) || internal.BootWarningNameFirewallDoorReachUnknown != ignored.BootWarningsIgnored()[0] {
        t.Fatalf("expected the trimmed code, got %v", ignored.BootWarningsIgnored())
    }

    ignored.BootWarningsIgnored()[0] = "changed"
    if internal.BootWarningNameFirewallDoorReachUnknown != ignored.BootWarningsIgnored()[0] {
        t.Fatalf("writing into the returned list reached the configuration")
    }
}

func TestConfigurationBootWarningsIgnoredRefusesWhatSilencesNothing(t *testing.T) {
    for _, testCase := range []struct {
        expected string
        values   map[string]string
    }{
        {"boot warning code may not be empty", map[string]string{BootWarningsIgnoredKey: internal.BootWarningNameFirewallDoorReachUnknown + ","}},
        {"boot warning code is listed twice", map[string]string{BootWarningsIgnoredKey: internal.BootWarningNameFirewallDoorReachUnknown + "," + internal.BootWarningNameFirewallDoorReachUnknown}},
    } {
        expected := testCase.expected
        values := testCase.values

        _, err := newConfigurationFromValues(values)

        found := false
        for current := err; nil != current; current = errors.Unwrap(current) {
            if refusal, isRefusal := current.(*exception.Error); true == isRefusal && expected == refusal.Message() && BootWarningsIgnoredKey == refusal.Context()["environmentKey"] {
                found = true
            }
        }

        if false == found {
            t.Fatalf("expected %q for %v, got %v", expected, values, err)
        }
    }

    for _, code := range []string{"aws.s3.plaintextEndpoint", internal.BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage} {
        if _, err := newConfigurationFromValues(map[string]string{BootWarningsIgnoredKey: code}); nil != err {
            t.Fatalf("expected %q admitted when the configuration is built, got %v", code, err)
        }
    }
}
