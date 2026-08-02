package config

import (
    "errors"
    "fmt"
    "io"
    "log"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/exception"
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

func TestConfiguration_ConcurrentRegisterAndReadIsRaceFree(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    var waitGroup sync.WaitGroup

    /* @important RegisterRuntime mutates the shared parameters map at runtime, so the readers (Get/Names/Parameters) must take the read lock: under -race an unguarded reader racing the writer reports a data race, and without -race it is Go's non-recoverable "fatal error: concurrent map read and map write" */
    for writerIndex := 0; writerIndex < 8; writerIndex++ {
        waitGroup.Add(1)
        go func(index int) {
            defer waitGroup.Done()
            for iteration := 0; iteration < 50; iteration++ {
                func() {
                    defer func() { _ = recover() }()
                    configuration.RegisterRuntime(fmt.Sprintf("app.runtime_%d_%d", index, iteration), index)
                }()
            }
        }(writerIndex)
    }

    for readerIndex := 0; readerIndex < 8; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for iteration := 0; iteration < 200; iteration++ {
                _ = configuration.Get("app.runtime_0_0")
                _ = configuration.Names()
                _ = configuration.Parameters()
            }
        }()
    }

    waitGroup.Wait()
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

/* @info A value that escapes a literal percent with %% resolves to text of the shape %NAME%, which the post-resolution scan then rejected as an "unresolved placeholder" — failing the whole boot for a correctly escaped literal. */
func TestConfiguration_EscapedPercentLiteralDoesNotFailValidation(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{
        CliDescriptionKey: "%%APP_NAME%% stays literal",
    }}

    environment, environmentErr := NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    configuration, configurationErr := NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("a correctly escaped percent literal must not fail configuration validation: %v", configurationErr)
    }

    resolved := configuration.MustGet(KernelCliDescription).String()
    if "%APP_NAME% stays literal" != resolved {
        t.Fatalf("expected the escaped percents to unescape, got %q", resolved)
    }
}

/* @info a parameter registered after the boot resolution keeps no raw template: it is resolved on registration against the parameters boot left in place, so a %env(...)% does not reach the consuming service verbatim */
func TestRegisterRuntime_AfterResolveResolvesTheTemplate(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{"MAIL_HOST": "smtp.example.com"}}

    environment, environmentErr := NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    configuration.RegisterRuntime("mail.host", "%env(MAIL_HOST)%")

    if "smtp.example.com" != configuration.MustGet("mail.host").MustString() {
        t.Fatalf("expected the post-boot template to resolve, got %q", configuration.MustGet("mail.host").MustString())
    }
}

/* @info the composition root registers its parameters between construction and boot, so a forward reference there must survive to the boot pass instead of being resolved eagerly against parameters that do not exist yet */
func TestRegisterRuntime_BeforeBootLeavesAForwardReferenceForTheBootPass(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, environmentErr := NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    configuration.RegisterRuntime("app.url", "%app.host%/api")
    configuration.RegisterRuntime("app.host", "https://acme.test")

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if "https://acme.test/api" != configuration.MustGet("app.url").MustString() {
        t.Fatalf("expected the forward reference to resolve at boot, got %q", configuration.MustGet("app.url").MustString())
    }
}

/* @info runtime secret marking */

func TestRegisterRuntimeSecret_MarksOnlyTheDeclaredParameter(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{
            "CLIENT_ID":     "public-identifier",
            "CLIENT_SECRET": "P4ssPhrase",
        },
        func(configuration *Configuration) {
            configuration.RegisterRuntime("app.client_id", "%env(CLIENT_ID)%")
            configuration.RegisterRuntimeSecret("app.client_secret", "%env(CLIENT_SECRET)%")
        },
    )

    expectations := map[string]bool{
        "app.client_id":     false,
        "app.client_secret": true,
    }

    for name, expectedSecret := range expectations {
        parameter := configuration.Get(name)
        if nil == parameter {
            t.Fatalf("expected parameter %q to exist", name)
        }

        if expectedSecret != parameter.IsSecret() {
            t.Fatalf("expected %q secret marking to be %t", name, expectedSecret)
        }
    }
}

/* @info the marking governs display only: a service that consumes the parameter still receives the credential in full */
func TestRegisterRuntimeSecret_LeavesTheValueIntact(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{"CLIENT_SECRET": "P4ssPhrase"},
        func(configuration *Configuration) {
            configuration.RegisterRuntimeSecret("app.client_secret", "%env(CLIENT_SECRET)%")
        },
    )

    if "P4ssPhrase" != configuration.Get("app.client_secret").String() {
        t.Fatalf("expected the secret value to resolve in full for its consumers")
    }
}

/* @info a dsn assembled from a declared password holds the credential in full; without propagation the password would be redacted while the dsn beside it printed in clear */
func TestRegisterRuntimeSecret_PropagatesToParametersThatReadIt(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{"DATABASE_PASSWORD": "P4ssPhrase"},
        func(configuration *Configuration) {
            configuration.RegisterRuntimeSecret("database.password", "%env(DATABASE_PASSWORD)%")
            configuration.RegisterRuntime("database.dsn", "postgres://app:%database.password%@db:5432/app")
        },
    )

    dsnParameter := configuration.Get("database.dsn")
    if nil == dsnParameter {
        t.Fatalf("expected the dsn parameter to exist")
    }

    if "postgres://app:P4ssPhrase@db:5432/app" != dsnParameter.String() {
        t.Fatalf("expected the dsn to resolve in full, got %q", dsnParameter.String())
    }

    if false == dsnParameter.IsSecret() {
        t.Fatalf("expected the dsn to inherit the secret marking of the password it reads")
    }
}

func TestRegisterRuntime_LeavesAnOrdinaryParameterUnmarked(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{"API_URL": "https://api.example.test"},
        func(configuration *Configuration) {
            configuration.RegisterRuntime("app.api_url", "%env(API_URL)%")
        },
    )

    if true == configuration.Get("app.api_url").IsSecret() {
        t.Fatalf("expected an ordinary parameter to stay unmarked")
    }
}

/* @info a MarkSecret arriving after the boot resolve travels to the parameters whose templates read the key, exactly as the early marking does: without the retroactive scan the key was redacted while the dsn assembled from it printed in full */
func TestMarkSecret_PropagatesRetroactivelyToDirectReaders(t *testing.T) {
    environment := &Environment{values: map[string]string{
        "DB_PASSWORD": "hunter2",
    }}

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("configuration error: %v", err)
    }

    configuration.RegisterRuntime("database.dsn", "postgres://app:%env(DB_PASSWORD)%@db/app")

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("resolve error: %v", resolveErr)
    }

    if true == configuration.MustGet("database.dsn").IsSecret() {
        t.Fatalf("expected the dsn to start unmarked")
    }

    if false == configuration.MarkSecret("DB_PASSWORD") {
        t.Fatalf("expected the marking to land")
    }

    if false == configuration.MustGet("DB_PASSWORD").IsSecret() {
        t.Fatalf("expected the key itself to be marked")
    }

    if false == configuration.MustGet("database.dsn").IsSecret() {
        t.Fatalf("expected the late marking to travel to the parameter assembled from the key")
    }
}

/* @info a runtime registration that fails to resolve leaves nothing behind: publishing before resolving served the raw template to every reader that outlived the recovered panic and burnt the name for the corrected retry */
func TestRegisterRuntime_FailedResolutionLeavesNoHalfMadeParameter(t *testing.T) {
    environment := &Environment{values: map[string]string{}}

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("configuration error: %v", err)
    }

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("resolve error: %v", resolveErr)
    }

    func() {
        defer func() {
            if recoveredValue := recover(); nil == recoveredValue {
                t.Fatalf("expected the unresolvable registration to panic")
            }
        }()

        configuration.RegisterRuntime("mail.dsn", "%env(UNDEFINED_MAIL_DSN)%")
    }()

    if nil != configuration.Get("mail.dsn") {
        t.Fatalf("expected the failed registration to leave no parameter behind")
    }

    configuration.RegisterRuntime("mail.dsn", "smtp://mail.internal")
    if "smtp://mail.internal" != configuration.MustGet("mail.dsn").String() {
        t.Fatalf("expected the corrected retry to register cleanly")
    }
}

/* @info a name is judged trimmed: the padded spelling would register a parameter no exact-match lookup ever names, and the whitespace-only name passed the empty guard as a phantom */
func TestRegisterRuntime_RefusesWhitespaceNames(t *testing.T) {
    environment := &Environment{values: map[string]string{}}

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("configuration error: %v", err)
    }

    func() {
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                t.Fatalf("expected the whitespace-only name to be refused")
            }

            /* the whitespace-only name is refused AS EMPTY, through the guard that names the real problem — the padding guard would also refuse it, with a message that sends the operator hunting for stray spaces around a name that does not exist at all */
            recoveredErr, isError := recoveredValue.(error)
            if false == isError || false == strings.Contains(recoveredErr.Error(), "empty names") {
                t.Fatalf("expected the empty-name refusal for a whitespace-only name, got: %v", recoveredValue)
            }
        }()

        configuration.RegisterRuntime("   ", "value")
    }()

    func() {
        defer func() {
            if recoveredValue := recover(); nil == recoveredValue {
                t.Fatalf("expected the padded name to be refused")
            }
        }()

        configuration.RegisterRuntime(" mail.dsn ", "value")
    }()
}

/* @info a runtime parameter is named in its conversion errors: identified only by its empty environmentKey it was anonymous, and "cannot convert" named nothing an operator could find */
func TestRuntimeParameter_ConversionErrorNamesTheParameter(t *testing.T) {
    environment := &Environment{values: map[string]string{}}

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("configuration error: %v", err)
    }

    configuration.RegisterRuntime("app.pool_size", "not-a-number")

    _, intErr := configuration.MustGet("app.pool_size").Int()
    if nil == intErr {
        t.Fatalf("expected the conversion to fail")
    }

    var exceptionErr *exception.Error
    if false == errors.As(intErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got: %v", intErr)
    }
    if "app.pool_size" != exceptionErr.Context()["parameterName"] {
        t.Fatalf("expected the parameter name in the error context, got: %v", exceptionErr.Context())
    }
}

/* TestMain silences the standard logger for the whole package: the configuration reports through it on
several paths, and the output belongs to the code under test rather than to the test run. */
func TestMain(mainInstance *testing.M) {
    log.SetOutput(io.Discard)
    os.Exit(mainInstance.Run())
}

/* newResolvedConfiguration builds a configuration over a fixed environment, lets the caller declare its
parameters and resolves them, which is the shape almost every test of this file and of the resolve path
needs before it can assert anything. */
func newResolvedConfiguration(
    t *testing.T,
    environmentValues map[string]string,
    declare func(configuration *Configuration),
) *Configuration {
    t.Helper()

    configuration, newConfigurationErr := NewConfiguration(
        &Environment{values: environmentValues},
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the configuration to build, got %v", newConfigurationErr)
    }

    declare(configuration)

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the parameters to resolve, got %v", resolveErr)
    }

    return configuration
}
