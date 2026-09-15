package config

import (
    "errors"
    "fmt"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

func TestResolveTemplate_SelfReferentialEnvValueReportsCircularReference(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "APP_A": "x%env(APP_A)%",
            },
        },
        parameters: ParameterMap{},
    }

    type resolveOutcome struct {
        value string
        err   error
    }

    done := make(chan resolveOutcome, 1)

    go func() {
        value, err := configuration.resolveTemplate(
            "x%env(APP_A)%",
            "app.a",
            make(map[string]bool),
            make(map[string]bool),
        )

        done <- resolveOutcome{value: value, err: err}
    }()

    select {
    case outcome := <-done:
        if nil == outcome.err {
            t.Fatalf("expected a circular reference error, got value %q", outcome.value)
        }

        if false == strings.Contains(outcome.err.Error(), "circular reference") {
            t.Fatalf("expected a circular reference error, got %v", outcome.err)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("resolveTemplate never returned: the self-referential env value looped forever")
    }
}

func TestResolveTemplate_UndefinedEnvironmentKeyErrorOmitsRawValue(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    secretValue := "mysql://root:S3cretPassword@tcp(db:3306)/app?tls=%env(DB_TLS)%"

    _, err := configuration.resolveTemplate(secretValue, "database.url", make(map[string]bool), make(map[string]bool))
    if nil == err {
        t.Fatalf("expected an undefined environment key error")
    }

    context := contextOfError(t, err)

    assertContextOmitsSecret(t, context, "S3cretPassword")

    if "DB_TLS" != context["environmentKey"] {
        t.Fatalf("expected the offending environment key in the context, got %v", context["environmentKey"])
    }
}

func TestResolveTemplate_UndefinedParameterKeyErrorOmitsRawValue(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    secretValue := "postgres://user:P4ssPhrase@host:5432/db?opt=%missing_parameter%"

    _, err := configuration.resolveTemplate(secretValue, "database.dsn", make(map[string]bool), make(map[string]bool))
    if nil == err {
        t.Fatalf("expected an undefined parameter key error")
    }

    context := contextOfError(t, err)

    assertContextOmitsSecret(t, context, "P4ssPhrase")

    if "missing_parameter" != context["parameterKey"] {
        t.Fatalf("expected the offending parameter key in the context, got %v", context["parameterKey"])
    }
}

func contextOfError(t *testing.T, err error) map[string]any {
    t.Helper()

    exceptionError, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected an *exception.Error, got %T", err)
    }

    return exceptionError.Context()
}

func assertContextOmitsSecret(t *testing.T, context map[string]any, secret string) {
    t.Helper()

    if _, present := context["value"]; true == present {
        t.Fatalf("error context must not embed the raw parameter value")
    }

    if true == strings.Contains(fmt.Sprintf("%v", context), secret) {
        t.Fatalf("error context leaked the inline credential: %v", context)
    }
}

func TestEnvPlaceholderPattern_AcceptsDefaultProcessorForms(t *testing.T) {
    if false == envPlaceholderPattern.MatchString("%env(default::AWS_ENDPOINT_URL)%") {
        t.Fatalf("expected pattern to accept the empty fallback form")
    }

    if false == envPlaceholderPattern.MatchString("%env(default:app.fallback:AWS_ENDPOINT_URL)%") {
        t.Fatalf("expected pattern to accept the parameter fallback form")
    }
}

func TestResolveTemplate_MalformedEnvPlaceholderIsReported(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    _, err := configuration.resolveTemplate(
        "%env(default:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == err {
        t.Fatalf("expected a malformed placeholder error")
    }

    context := contextOfError(t, err)

    if "%env(default:AWS_ENDPOINT_URL)%" != context["placeholder"] {
        t.Fatalf("expected the offending placeholder in the context, got %v", context["placeholder"])
    }
}

func TestResolveTemplate_DefaultProcessorYieldsEmptyStringForUndefinedKey(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    value, err := configuration.resolveTemplate(
        "%env(default::AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != err {
        t.Fatalf("expected the undefined key to be tolerated, got %v", err)
    }

    if "" != value {
        t.Fatalf("expected an empty value, got %q", value)
    }
}

func TestResolveTemplate_DefaultProcessorFallsBackToParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{
            "aws.default_endpoint": NewParameter("", "http://localstack:4566", "http://localstack:4566", false),
        },
    }

    value, err := configuration.resolveTemplate(
        "%env(default:aws.default_endpoint:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != err {
        t.Fatalf("expected the fallback parameter to resolve, got %v", err)
    }

    if "http://localstack:4566" != value {
        t.Fatalf("expected the fallback parameter value, got %q", value)
    }
}

func TestResolveTemplate_DefaultProcessorPrefersDefinedEnvironmentValue(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "AWS_ENDPOINT_URL": "https://s3.eu-central-1.amazonaws.com",
            },
        },
        parameters: ParameterMap{
            "aws.default_endpoint": NewParameter("", "http://localstack:4566", "http://localstack:4566", false),
        },
    }

    value, err := configuration.resolveTemplate(
        "%env(default:aws.default_endpoint:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != err {
        t.Fatalf("expected resolution to succeed, got %v", err)
    }

    if "https://s3.eu-central-1.amazonaws.com" != value {
        t.Fatalf("expected the environment value to win over the fallback, got %q", value)
    }
}

func TestResolveTemplate_PlainPlaceholderStillFailsForUndefinedKey(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    _, err := configuration.resolveTemplate(
        "%env(AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == err {
        t.Fatalf("expected an undefined environment key error")
    }

    context := contextOfError(t, err)

    if "AWS_ENDPOINT_URL" != context["environmentKey"] {
        t.Fatalf("expected the offending environment key in the context, got %v", context["environmentKey"])
    }
}

func TestResolveTemplate_DefaultProcessorReportsUndefinedFallbackParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    _, err := configuration.resolveTemplate(
        "%env(default:aws.missing_fallback:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == err {
        t.Fatalf("expected an undefined parameter key error")
    }

    context := contextOfError(t, err)

    if "aws.missing_fallback" != context["parameterKey"] {
        t.Fatalf("expected the offending fallback key in the context, got %v", context["parameterKey"])
    }
}

func TestConfiguration_EnvironmentValueWithLiteralPercentIsWrittenDoubled(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "DATABASE_PASSWORD": "pa%%ss%%word",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the escaped value to resolve, got %v", newConfigurationErr)
    }

    if "pa%ss%word" != configuration.Get("DATABASE_PASSWORD").String() {
        t.Fatalf("unexpected resolved value %q", configuration.Get("DATABASE_PASSWORD").String())
    }
}

func TestConfiguration_UnescapedLiteralPercentFailsTheBootResolveWithAnActionableMessage(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "DATABASE_PASSWORD": "pa%ss%word",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the constructor to defer the unresolved reference, got %v", newConfigurationErr)
    }

    resolveErr := configuration.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected the boot resolve to fail on the unescaped value")
    }

    messages := ""
    for err := resolveErr; nil != err; err = errors.Unwrap(err) {
        messages = messages + err.Error() + "\n"
    }

    if false == strings.Contains(messages, "%%") {
        t.Fatalf("expected the cause chain to point at the escape, got %s", messages)
    }
}

func TestConfiguration_AForwardReferenceDefersAndTheBootResolveSettlesIt(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "APP_GREETING": "hello %app.user%",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the constructor to defer the forward reference, got %v", newConfigurationErr)
    }

    configuration.RegisterRuntime("app.user", "operator")

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the boot resolve to settle the deferred reference, got %v", resolveErr)
    }

    if "hello operator" != configuration.Get("APP_GREETING").String() {
        t.Fatalf("unexpected resolved value %q", configuration.Get("APP_GREETING").String())
    }
}

func TestConfiguration_ADeferredParameterIsUnreadableUntilTheBootResolveSettlesIt(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "APP_GREETING": "hello %app.user%",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the constructor to defer the forward reference, got %v", newConfigurationErr)
    }

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the read of a deferred parameter to refuse")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %T", recoveredValue)
        }

        if false == strings.Contains(recoveredErr.Error(), "deferred to boot") {
            t.Fatalf("unexpected refusal message: %q", recoveredErr.Error())
        }
    }()

    _ = configuration.Get("APP_GREETING").String()
}

func TestResolveAll_TheTolerantPassDoesNotDeferAReservedParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{
            "kernel.probe": NewParameter("KERNEL_PROBE", "%missing.parameter%", nil, false),
        },
    }

    resolveErr := configuration.resolveAll(true)
    if nil == resolveErr {
        t.Fatalf("expected the tolerant pass to refuse an unresolved reference in a reserved parameter")
    }

    if false == strings.Contains(resolveErr.Error(), "failed to resolve parameter") {
        t.Fatalf("unexpected refusal message: %q", resolveErr.Error())
    }
}

func TestResolveTemplate_EnvValueDoubledPercentsResolveToLiterals(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "APP_PASSWORD": "pa%%ss%%word",
            },
        },
        parameters: ParameterMap{},
    }

    value, resolveErr := configuration.resolveTemplate(
        "%env(APP_PASSWORD)%",
        "app.password",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the env value to resolve, got %v", resolveErr)
    }

    if "pa%ss%word" != value {
        t.Fatalf("expected the doubled percents to resolve to literals, got %q", value)
    }
}

func TestResolveTemplate_ReferencedValueWithLiteralPercentSplicesAsData(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.password": NewParameter("APP_PASSWORD", "pa%%ss%%word", "pa%%ss%%word", false),
        },
    }

    escapedValue, resolveErr := configuration.resolveTemplate(
        "mysql://root:%app.password%@db/app",
        "database.url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the reference to resolve, got %v", resolveErr)
    }

    if "mysql://root:pa%ss%word@db/app" != escapedValue {
        t.Fatalf("expected the referenced percents to survive as data, got %q", escapedValue)
    }
}

func TestResolveTemplate_SecretTravelsThroughAnEnvPlaceholder(t *testing.T) {
    passwordParameter := NewParameter("MYSQL_PASSWORD", "s3cret", "s3cret", false)
    passwordParameter.isSecret.Store(true)

    databaseUrlParameter := NewParameter("", "root:%env(MYSQL_PASSWORD)%@db", "", false)

    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "MYSQL_PASSWORD": "s3cret",
            },
        },
        parameters: ParameterMap{
            "MYSQL_PASSWORD": passwordParameter,
            "database.url":   databaseUrlParameter,
        },
    }

    escapedValue, resolveErr := configuration.resolveTemplate(
        "root:%env(MYSQL_PASSWORD)%@db",
        "database.url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the env reference to resolve, got %v", resolveErr)
    }

    if "root:s3cret@db" != escapedValue {
        t.Fatalf("unexpected resolved value %q", escapedValue)
    }

    if false == databaseUrlParameter.IsSecret() {
        t.Fatalf("expected the secret marking to travel through the env placeholder")
    }
}

func TestResolveTemplate_InjectedMalformedShapeStillFails(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "APP_RAW": "x%env(a:b)%y",
            },
        },
        parameters: ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "%env(APP_RAW)%",
        "app.raw",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the malformed injected placeholder to be reported")
    }

    if false == strings.Contains(resolveErr.Error(), "malformed environment placeholder") {
        t.Fatalf("unexpected error: %v", resolveErr)
    }
}

func TestResolveTemplate_SelfReferenceIsACircularReference(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.a": NewParameter("", "%app.b%_%app.a%", "", false),
            "app.b": NewParameter("", "pa%%ss", "", false),
        },
    }

    resolveErr := configuration.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected the self-reference to be reported at resolve time")
    }

    if false == strings.Contains(resolveErr.Error()+causeChain(resolveErr), "circular parameter reference") {
        t.Fatalf("unexpected error: %v", resolveErr)
    }
}

func causeChain(err error) string {
    messages := ""
    for cause := err; nil != cause; cause = errors.Unwrap(cause) {
        messages = messages + cause.Error() + "\n"
    }

    return messages
}

func TestResolveTemplate_SplicedLiteralsAndAdjacentReferencesResolve(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.a": NewParameter("", "x_%app.b%", "", false),
            "app.b": NewParameter("", "pa%%ss%%word", "", false),
            "app.c": NewParameter("", "%app.a%%app.b%", "", false),
        },
    }

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the resolution to succeed, got %v", resolveErr)
    }

    if "x_pa%ss%word" != configuration.getInternalParameter("app.a").String() {
        t.Fatalf("unexpected resolved value %q", configuration.getInternalParameter("app.a").String())
    }

    if "x_pa%ss%wordpa%ss%word" != configuration.getInternalParameter("app.c").String() {
        t.Fatalf("unexpected adjacent resolution %q", configuration.getInternalParameter("app.c").String())
    }
}

func TestResolveTemplate_SingleCharacterParameterReferenceResolves(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "a": NewParameter("", "short", "", false),
        },
    }

    escapedValue, resolveErr := configuration.resolveTemplate(
        "%env(default:a:MISSING_KEY)%",
        "app.value",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the single-character fallback to resolve, got %v", resolveErr)
    }

    if "short" != escapedValue {
        t.Fatalf("expected the fallback parameter to resolve, got %q", escapedValue)
    }
}

func TestResolveTemplate_LiteralEnvPrefixBesideARealPlaceholderIsData(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "APP_NAME": "melody",
            },
        },
        parameters: ParameterMap{},
    }

    value, resolveErr := configuration.resolveTemplate(
        "write %env( around the key; app=%env(APP_NAME)%",
        "app.hint",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the literal prefix to stay data, got %v", resolveErr)
    }

    if "write %env( around the key; app=melody" != value {
        t.Fatalf("unexpected resolved value %q", value)
    }
}

func TestResolveTemplate_ProjectDirectoryReferenceIsData(t *testing.T) {
    projectDirectoryParameter := NewParameter("", "/srv/app%1", "/srv/app%1", true)

    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            KernelProjectDir:  projectDirectoryParameter,
            "kernel.logs_dir": NewParameter("", "%kernel.project_dir%/var/log", "", true),
        },
    }

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the reference to resolve, got %v", resolveErr)
    }

    if "/srv/app%1/var/log" != configuration.getInternalParameter("kernel.logs_dir").String() {
        t.Fatalf("unexpected logs dir %q", configuration.getInternalParameter("kernel.logs_dir").String())
    }

    if "/srv/app%1" != projectDirectoryParameter.String() {
        t.Fatalf("expected the project directory to stay untouched, got %q", projectDirectoryParameter.String())
    }
}

func TestResolveTemplate_ClosedMisspelledPlaceholderIsStillReported(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "%env(FOO-BAR)%",
        "app.value",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the closed misspelled placeholder to be reported")
    }

    if false == strings.Contains(resolveErr.Error(), "malformed environment placeholder") {
        t.Fatalf("unexpected error: %v", resolveErr)
    }
}

func TestRegisterRuntime_ConcurrentWithReadsOfAReferencedParameter(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{"ACME_HOST": "acme.example"},
        func(configuration *Configuration) {
            configuration.RegisterRuntime("app.host", "%env(ACME_HOST)%")
        },
    )

    var waitGroup sync.WaitGroup
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for iteration := 0; iteration < 1000; iteration++ {
            configuration.RegisterRuntime(fmt.Sprintf("app.derived.%d", iteration), "%app.host%:8080")
        }
    }()

    go func() {
        defer waitGroup.Done()

        for iteration := 0; iteration < 1000; iteration++ {
            if "acme.example" != configuration.Get("app.host").String() {
                t.Errorf("expected the referenced parameter to keep its resolved value")

                return
            }
        }
    }()

    waitGroup.Wait()
}

func TestConfiguration_ResolveIsRefusedOnceServing(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "APP_TAG": "tag",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the configuration to build, got %v", newConfigurationErr)
    }

    configuration.RegisterRuntime("app.tag", "%env(APP_TAG)%")

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("expected the pre-serving resolve to succeed, got %v", resolveErr)
    }

    configuration.MarkServing()

    resolveErr := configuration.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected a resolve to be refused once the application serves")
    }

    if false == strings.Contains(resolveErr.Error(), "begun serving") {
        t.Fatalf("expected the refusal to say why, got %q", resolveErr.Error())
    }

    if "tag" != configuration.MustGet("app.tag").MustString() {
        t.Fatalf("expected the value resolved before serving to be untouched, got %q", configuration.MustGet("app.tag").MustString())
    }
}

func TestConfiguration_RegisterRuntimeStillResolvesOnceServing(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "APP_TAG": "tag",
            },
        },
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the configuration to build, got %v", newConfigurationErr)
    }

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("expected the pre-serving resolve to succeed, got %v", resolveErr)
    }

    configuration.MarkServing()

    configuration.RegisterRuntime("app.late", "%env(APP_TAG)%")

    if "tag" != configuration.MustGet("app.late").MustString() {
        t.Fatalf("expected the late parameter to resolve on registration, got %q", configuration.MustGet("app.late").MustString())
    }
}

func TestResolveTemplate_EnvPlaceholderWithInnerParenthesisIsRefused(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{"A": "x"}},
        parameters:  ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "%env(A))%",
        "app.a",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the placeholder with the inner parenthesis to be refused")
    }
    if false == strings.Contains(resolveErr.Error(), "malformed environment placeholder") {
        t.Fatalf("expected the malformed placeholder report, got: %v", resolveErr)
    }
}

func TestResolveTemplate_UnterminatedEnvPlaceholderIsRefused(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{"DB_PASS": "secret"}},
        parameters:  ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "postgres://user:%env(DB_PASS)@db/app",
        "database.url",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the unterminated placeholder to be refused")
    }
    if false == strings.Contains(resolveErr.Error(), "unterminated environment placeholder") {
        t.Fatalf("expected the unterminated placeholder report, got: %v", resolveErr)
    }
}

func TestResolveTemplate_UnclosedParameterReferenceIsRefused(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.name": NewParameter("", "melody", "melody", false),
        },
    }

    _, resolveErr := configuration.resolveTemplate(
        "service-%app-name%",
        "app.banner",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the unclosed reference to be refused")
    }
    if false == strings.Contains(resolveErr.Error(), "malformed parameter reference") {
        t.Fatalf("expected the malformed reference report, got: %v", resolveErr)
    }

    if "%app" != contextOfError(t, resolveErr)["reference"] {
        t.Fatalf("expected the name-shaped run to be the refused reference, got: %v", contextOfError(t, resolveErr)["reference"])
    }
}

func TestResolveTemplate_PercentBeforeNonNameCharacterStaysData(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    value, resolveErr := configuration.resolveTemplate(
        "growth of 50% overall",
        "app.note",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the literal percent to survive, got: %v", resolveErr)
    }
    if "growth of 50% overall" != value {
        t.Fatalf("unexpected value: %q", value)
    }
}

func TestResolveTemplate_NonStringReferenceErrorOmitsTheValue(t *testing.T) {
    secretBytes := "0123456789abcdef"

    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.signing_key": NewParameter("", []byte(secretBytes), []byte(secretBytes), false),
        },
    }

    _, resolveErr := configuration.resolveTemplate(
        "key=%app.signing_key%",
        "app.assembled",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the non-string reference to be refused")
    }

    var exceptionErr *exception.Error
    if false == errors.As(resolveErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got: %v", resolveErr)
    }
    if _, valuePresent := exceptionErr.Context()["environmentValue"]; true == valuePresent {
        t.Fatalf("expected the raw value to stay out of the error context")
    }
    if "[]uint8" != exceptionErr.Context()["environmentValueType"] {
        t.Fatalf("expected the type to identify the value, got: %v", exceptionErr.Context()["environmentValueType"])
    }
    if true == strings.Contains(fmt.Sprintf("%v", exceptionErr.Context()), secretBytes) {
        t.Fatalf("expected the secret bytes to stay out of the rendered context")
    }
}

func TestResolveAll_TwoBrokenTemplatesFailOnTheSortedFirstParameterEveryTime(t *testing.T) {
    for iteration := 0; iteration < 30; iteration++ {
        configuration := &Configuration{
            environment: &Environment{
                values: map[string]string{},
            },
            parameters: ParameterMap{
                "zulu.broken":  NewParameter("ZULU_BROKEN", "%missing.one%", nil, false),
                "alpha.broken": NewParameter("ALPHA_BROKEN", "%missing.two%", nil, false),
            },
        }

        resolveErr := configuration.resolveAll(false)
        if nil == resolveErr {
            t.Fatalf("expected the broken templates to be refused")
        }

        melodyErr, isMelodyErr := resolveErr.(*exception.Error)
        if false == isMelodyErr {
            t.Fatalf("expected a melody error, got %T", resolveErr)
        }

        if "alpha.broken" != melodyErr.Context()["parameter"] {
            t.Fatalf("iteration %d: expected the failure to name the sorted-first parameter, got %v", iteration, melodyErr.Context()["parameter"])
        }
    }
}

func TestResolve_TheFailureNamesTheEnvironmentKeyBesideTheInternalAlias(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            "MELODY_LOG_PATH": "%app.name%/application.log",
        },
    }

    environment, environmentErr := NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    _, configurationErr := NewConfiguration(environment, t.TempDir())
    if nil == configurationErr {
        t.Fatal("expected the undefined reference to fail the construction")
    }

    resolutionContext := map[string]any(nil)

    for current := error(configurationErr); nil != current; current = errors.Unwrap(current) {
        melodyErr, isMelodyErr := current.(*exception.Error)
        if false == isMelodyErr {
            continue
        }

        if "failed to resolve parameter" == melodyErr.Error() {
            resolutionContext = melodyErr.Context()

            break
        }
    }

    if nil == resolutionContext {
        t.Fatalf("expected the parameter resolution failure in the chain, got %v", configurationErr)
    }

    if KernelLogPath != resolutionContext["parameter"] {
        t.Fatalf("expected the internal alias kept, got %v", resolutionContext["parameter"])
    }

    if "MELODY_LOG_PATH" != resolutionContext["environmentKey"] {
        t.Fatalf("expected the key the operator actually wrote, got %v", resolutionContext["environmentKey"])
    }
}
