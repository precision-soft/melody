package config

import (
    "fmt"
    "path/filepath"
    "testing"
)

func TestConfigurationDefaultsAndTemplateResolution(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    projectDir := filepath.Join("/tmp", "melody")
    configuration, err := NewConfiguration(environment, projectDir)
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if projectDir != configuration.Kernel().ProjectDir() {
        t.Fatalf("expected project dir to be resolved")
    }

    expectedLogsDir := filepath.Join(projectDir, "var", "log")
    if expectedLogsDir != configuration.MustGet(KernelLogsDir).String() {
        t.Fatalf("expected logs dir template to be resolved")
    }

    expectedLogPath := filepath.Join(expectedLogsDir, EnvDevelopment+".log")
    if expectedLogPath != configuration.MustGet(KernelLogPath).String() {
        t.Fatalf("expected log path template to be resolved")
    }

    if ModeHttp != configuration.Kernel().DefaultMode() {
        t.Fatalf("expected default mode http")
    }
}

func TestConfigurationEnvironmentOverridesDefaultsWhenNonEmpty(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            EnvKey:         EnvProduction,
            HttpAddressKey: ":9090",
        },
    }

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if EnvProduction != configuration.Kernel().Env() {
        t.Fatalf("expected environment to be overridden")
    }

    if ":9090" != configuration.MustGet(KernelHttpAddress).String() {
        t.Fatalf("expected http address to be overridden")
    }
}

func TestConfigurationMustGetMissingPanics(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    defer func() {
        if nil == recover() {
            t.Fatalf("expected panic")
        }
    }()

    _ = configuration.MustGet("missing.parameter")
}

func TestConfigurationRegisterRuntimeValidationPanics(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    func() {
        defer func() {
            if nil == recover() {
                t.Fatalf("expected panic for empty name")
            }
        }()

        configuration.RegisterRuntime("", "x")
    }()

    func() {
        defer func() {
            if nil == recover() {
                t.Fatalf("expected panic for reserved prefix")
            }
        }()

        configuration.RegisterRuntime("kernel.forbidden", "x")
    }()

    func() {
        defer func() {
            if nil == recover() {
                t.Fatalf("expected panic for duplicate name")
            }
        }()

        configuration.RegisterRuntime("runtime.value", "1")
        configuration.RegisterRuntime("runtime.value", "2")
    }()
}

func TestConfigurationRegisterRuntime_SuccessfullyRegisters(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    configuration.RegisterRuntime("app.custom_value", "hello")

    parameter := configuration.Get("app.custom_value")
    if nil == parameter {
        t.Fatalf("expected parameter to exist after RegisterRuntime")
    }

    if "hello" != parameter.String() {
        t.Fatalf("expected parameter value 'hello', got: %s", parameter.String())
    }
}

func TestConfigurationRegisterRuntime_ConcurrentCallsDoNotPanic(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    done := make(chan bool, 10)

    for i := 0; i < 10; i++ {
        go func(index int) {
            defer func() {
                _ = recover()
                done <- true
            }()

            name := "app.concurrent_" + filepath.Base(fmt.Sprintf("%d", index))
            configuration.RegisterRuntime(name, index)
        }(i)
    }

    for i := 0; i < 10; i++ {
        <-done
    }
}

func TestRegisterRuntimeAddsValue(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    configuration.RegisterRuntime("runtime.test", "x")

    if "x" != configuration.MustGet("runtime.test").String() {
        t.Fatalf("expected runtime value to be visible")
    }
}

/* @info placeholder patterns */

func TestEnvPlaceholderPattern_RejectsIdentifiersStartingWithDigit(t *testing.T) {
    if true == envPlaceholderPattern.MatchString("%env(1INVALID)%") {
        t.Fatalf("expected pattern to reject identifier starting with digit")
    }
}

func TestEnvPlaceholderPattern_AcceptsIdentifiersStartingWithLetterOrUnderscore(t *testing.T) {
    if false == envPlaceholderPattern.MatchString("%env(VALID_KEY)%") {
        t.Fatalf("expected pattern to accept identifier starting with letter")
    }

    if false == envPlaceholderPattern.MatchString("%env(_VALID)%") {
        t.Fatalf("expected pattern to accept identifier starting with underscore")
    }
}

func TestParameterPlaceholderPattern_RejectsIdentifiersStartingWithDigit(t *testing.T) {
    if true == parameterPlaceholderPattern.MatchString("%1invalid%") {
        t.Fatalf("expected pattern to reject identifier starting with digit")
    }
}

func TestParameterPlaceholderPattern_AcceptsDottedIdentifiers(t *testing.T) {
    if false == parameterPlaceholderPattern.MatchString("%kernel.project_dir%") {
        t.Fatalf("expected pattern to accept dotted identifier")
    }
}
