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
    "time"

    "github.com/precision-soft/melody/v3/exception"
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

func TestConfigurationRegisterRuntime_PreBootTemplateIsDeferredThenResolves(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{"APP_URL": "https://example.test"}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    configuration.RegisterRuntime("app.callback", "%env(APP_URL)%/callback")

    parameter := configuration.Get("app.callback")
    if nil == parameter {
        t.Fatalf("expected parameter to exist after RegisterRuntime")
    }

    func() {
        defer func() {
            if nil == recover() {
                t.Fatalf("expected a pre-boot read of a templated runtime parameter to refuse, not serve the raw template")
            }
        }()

        _ = parameter.String()
    }()

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if "https://example.test/callback" != parameter.String() {
        t.Fatalf("expected the resolved value after boot, got: %s", parameter.String())
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

func TestMarkSecret_PropagatesRetroactivelyThroughDerivationChains(t *testing.T) {
    environment := &Environment{values: map[string]string{
        "G6_SECRET": "hunter2",
        "G6_MIDDLE": "x-%env(G6_SECRET)%",
        "G6_OUTER":  "y-%env(G6_MIDDLE)%",
    }}

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("configuration error: %v", err)
    }

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("resolve error: %v", resolveErr)
    }

    if true == configuration.MustGet("G6_OUTER").IsSecret() {
        t.Fatalf("expected the two-hop derivation to start unmarked")
    }

    if false == configuration.MarkSecret("G6_SECRET") {
        t.Fatalf("expected the marking to land")
    }

    if false == configuration.MustGet("G6_MIDDLE").IsSecret() {
        t.Fatalf("expected the direct reader to be marked")
    }

    if false == configuration.MustGet("G6_OUTER").IsSecret() {
        t.Fatalf("expected the mark to follow the chain to the two-hop derivation")
    }
}

func TestMarkSecret_ReachesAReaderSpelledWithTheKernelAlias(t *testing.T) {
    environment := &Environment{values: map[string]string{
        LogPathKey: "/var/log/app.log",
    }}

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("configuration error: %v", err)
    }

    configuration.RegisterRuntime("observability.sink", "file://%kernel.log_path%")

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("resolve error: %v", resolveErr)
    }

    if true == configuration.MustGet("observability.sink").IsSecret() {
        t.Fatalf("expected the alias-spelled reader to start unmarked")
    }

    if false == configuration.MarkSecret(LogPathKey) {
        t.Fatalf("expected the marking to land")
    }

    if false == configuration.MustGet(KernelLogPath).IsSecret() {
        t.Fatalf("expected the kernel spelling of the marked key to be marked")
    }

    if false == configuration.MustGet("observability.sink").IsSecret() {
        t.Fatalf("expected the late marking to reach the reader through the kernel alias")
    }
}

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

func TestMain(mainInstance *testing.M) {
    log.SetOutput(io.Discard)
    os.Exit(mainInstance.Run())
}

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

func TestRegisterRuntime_ALateParameterInheritsTheSecretMarkOfTheEnvironmentKeyItReads(t *testing.T) {
    configuration := newLateRegistrationConfiguration(t)

    configuration.RegisterRuntime("app.dsn.late", "postgres://user:%env(DB_PASSWORD)%@db/app")

    if false == configuration.MustGet("app.dsn.late").IsSecret() {
        t.Fatalf("expected the late parameter to inherit the marking of the credential it reads")
    }
}

func TestRegisterRuntime_ALateParameterInheritsTheSecretMarkOfTheParameterItReads(t *testing.T) {
    configuration := newLateRegistrationConfiguration(t)

    configuration.RegisterRuntime("app.dsn.viaParameter", "postgres://user:%DB_PASSWORD%@db/app")

    if false == configuration.MustGet("app.dsn.viaParameter").IsSecret() {
        t.Fatalf("expected the marking to travel through a parameter reference too")
    }
}

func TestRegisterRuntime_ALateParameterReadingNoCredentialStaysUnmarked(t *testing.T) {
    configuration := newLateRegistrationConfiguration(t)

    configuration.RegisterRuntime("app.endpoint.late", "https://api.internal/v1")

    if true == configuration.MustGet("app.endpoint.late").IsSecret() {
        t.Fatalf("expected an ordinary late parameter to stay unmarked")
    }
}

func newLateRegistrationConfiguration(t *testing.T) *Configuration {
    t.Helper()

    configuration, newConfigurationErr := NewConfiguration(
        &Environment{values: map[string]string{"DB_PASSWORD": "s3cr3t"}},
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("could not build the configuration: %v", newConfigurationErr)
    }

    if false == configuration.MarkSecret("DB_PASSWORD") {
        t.Fatalf("expected the environment key to be registered as a parameter")
    }

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("could not resolve: %v", resolveErr)
    }

    return configuration
}

func TestMarkSecret_PropagatesThroughAParameterReference(t *testing.T) {
    environment := &Environment{values: map[string]string{
        "DB_PASSWORD": "hunter2",
    }}

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("configuration error: %v", err)
    }

    configuration.RegisterRuntime("app.host", "api.internal")
    configuration.RegisterRuntime("app.endpoint", "https://%app.host%/v1")
    configuration.RegisterRuntime("database.dsn", "postgres://app:%DB_PASSWORD%@db/app")

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("resolve error: %v", resolveErr)
    }

    if true == configuration.MustGet("database.dsn").IsSecret() {
        t.Fatalf("expected the dsn to start unmarked")
    }

    if false == configuration.MarkSecret("DB_PASSWORD") {
        t.Fatalf("expected the marking to land")
    }

    if false == configuration.MustGet("database.dsn").IsSecret() {
        t.Fatalf("expected the late marking to travel through the parameter reference")
    }

    if true == configuration.MustGet("app.endpoint").IsSecret() {
        t.Fatalf("expected the reader of an unrelated parameter to stay unmarked")
    }
}

func TestConfigurationTeardownTimeoutDefaultsToTenSeconds(t *testing.T) {
    configuration := newTeardownTimeoutConfiguration(t, map[string]string{})

    teardownTimeout, teardownTimeoutErr := configuration.MustGet(KernelTeardownTimeout).Duration()
    if nil != teardownTimeoutErr {
        t.Fatalf("unexpected duration error: %v", teardownTimeoutErr)
    }

    if DefaultTeardownTimeout != teardownTimeout {
        t.Fatalf("expected the default teardown timeout, got %v", teardownTimeout)
    }
}

func TestConfigurationTeardownTimeoutIsReadFromTheEnvironment(t *testing.T) {
    configuration := newTeardownTimeoutConfiguration(t, map[string]string{TeardownTimeoutKey: "45s"})

    teardownTimeout, teardownTimeoutErr := configuration.MustGet(KernelTeardownTimeout).Duration()
    if nil != teardownTimeoutErr {
        t.Fatalf("unexpected duration error: %v", teardownTimeoutErr)
    }

    if 45*time.Second != teardownTimeout {
        t.Fatalf("expected the configured teardown timeout, got %v", teardownTimeout)
    }
}

func TestConfigurationTeardownTimeoutRejectsANegativeValue(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{TeardownTimeoutKey: "-1s"}}

    environment, environmentErr := NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    _, configurationErr := NewConfiguration(environment, "/tmp/melody")
    if nil == configurationErr {
        t.Fatalf("expected a negative teardown timeout to fail the boot")
    }
}

func TestConfigurationTeardownTimeoutAcceptsZeroAsNoDeadline(t *testing.T) {
    configuration := newTeardownTimeoutConfiguration(t, map[string]string{TeardownTimeoutKey: "0"})

    teardownTimeout, teardownTimeoutErr := configuration.MustGet(KernelTeardownTimeout).Duration()
    if nil != teardownTimeoutErr {
        t.Fatalf("unexpected duration error: %v", teardownTimeoutErr)
    }

    if 0 != teardownTimeout {
        t.Fatalf("expected zero to survive as zero, got %v", teardownTimeout)
    }
}

func TestConfigurationTeardownTimeoutRejectsAnUnparsableValue(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{TeardownTimeoutKey: "soon"}}

    environment, environmentErr := NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    _, configurationErr := NewConfiguration(environment, "/tmp/melody")
    if nil == configurationErr {
        t.Fatalf("expected an unparsable teardown timeout to fail the boot")
    }
}

func newTeardownTimeoutConfiguration(t *testing.T, values map[string]string) *Configuration {
    t.Helper()

    environment, environmentErr := NewEnvironment(&testEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    configuration, configurationErr := NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("new configuration error: %v", configurationErr)
    }

    return configuration
}
