package config

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "sync"
    "testing"
    "time"

    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/configseam"
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

func TestResolveTemplate_AnUndefinedReferenceInsideASecretParameterIsLocatedNotSpelled(t *testing.T) {
    passwordParameter := newParameter("APP_PASSWORD", "pa%ss%word", "pa%ss%word", false)
    passwordParameter.isSecret.Store(true)

    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{"app.password": passwordParameter},
    }

    _, err := configuration.resolveTemplate("pa%ss%word", "app.password", make(map[string]bool), make(map[string]bool))
    if nil == err {
        t.Fatalf("expected an undefined parameter key error")
    }

    context := contextOfError(t, err)

    if _, spelled := context["parameterKey"]; true == spelled {
        t.Fatalf("expected the reference of a secret value withheld, got %v", context)
    }

    if 2 != context["offset"] || 2 != context["parameterKeyLength"] {
        t.Fatalf("expected the reference located by offset and length, got %v", context)
    }
}

func TestResolveTemplate_AnUndefinedReferenceInsideAnEnvironmentValueIsLocatedNotSpelled(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{"DB_PASSWORD": "pa%ss%word"}},
        parameters:  ParameterMap{},
    }

    _, err := configuration.resolveTemplate("%env(DB_PASSWORD)%", "app.database.password", make(map[string]bool), make(map[string]bool))
    if nil == err {
        t.Fatalf("expected an undefined parameter key error")
    }

    context := contextOfError(t, err)

    if _, spelled := context["parameterKey"]; true == spelled {
        t.Fatalf("expected the reference of an environment value withheld, got %v", context)
    }

    if "DB_PASSWORD" != context["environmentKey"] || 2 != context["offset"] {
        t.Fatalf("expected the reference located in its environment value, got %v", context)
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

func TestConfiguration_EnvironmentValueWithLiteralPercentIsWrittenDoubled(t *testing.T) {
    configuration, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "DATABASE_PASSWORD": "pa%%ss%%word",
            },
        },
        "/srv/app",
        ConfigurationConfig{EnvironmentKeys: []configcontract.EnvironmentKey{applicationKey("DATABASE_PASSWORD", "app.database_password", "")}},
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the escaped value to resolve, got %v", newConfigurationErr)
    }

    if "pa%ss%word" != configuration.Get("app.database_password").String() {
        t.Fatalf("unexpected resolved value %q", configuration.Get("app.database_password").String())
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
        ConfigurationConfig{EnvironmentKeys: []configcontract.EnvironmentKey{applicationKey("DATABASE_PASSWORD", "app.database_password", "")}},
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
        ConfigurationConfig{EnvironmentKeys: []configcontract.EnvironmentKey{applicationKey("APP_GREETING", "app.greeting", "")}},
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the constructor to defer the forward reference, got %v", newConfigurationErr)
    }

    configuration.RegisterRuntime("app.user", "operator")

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the boot resolve to settle the deferred reference, got %v", resolveErr)
    }

    if "hello operator" != configuration.Get("app.greeting").String() {
        t.Fatalf("unexpected resolved value %q", configuration.Get("app.greeting").String())
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
        ConfigurationConfig{EnvironmentKeys: []configcontract.EnvironmentKey{applicationKey("APP_GREETING", "app.greeting", "")}},
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

    _ = configuration.Get("app.greeting").String()
}

func TestResolveAll_TheTolerantPassDoesNotDeferAReservedParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{
            "kernel.probe": newParameter("KERNEL_PROBE", "%missing.parameter%", nil, false),
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
            "app.password": newParameter("APP_PASSWORD", "pa%%ss%%word", "pa%%ss%%word", false),
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
    databaseUrlParameter := newParameter("", "root:%env(MYSQL_PASSWORD)%@db", "", false)

    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{
                "MYSQL_PASSWORD": "s3cret",
            },
        },
        parameters: ParameterMap{
            "database.url": databaseUrlParameter,
        },
        secretEnvironmentKeys: map[string]bool{"MYSQL_PASSWORD": true},
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
            "app.a": newParameter("", "%app.b%_%app.a%", "", false),
            "app.b": newParameter("", "pa%%ss", "", false),
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
            "app.a": newParameter("", "x_%app.b%", "", false),
            "app.b": newParameter("", "pa%%ss%%word", "", false),
            "app.c": newParameter("", "%app.a%%app.b%", "", false),
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
            "a": newParameter("", "short", "", false),
        },
    }

    escapedValue, resolveErr := configuration.resolveTemplate(
        "%a%",
        "app.value",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil != resolveErr {
        t.Fatalf("expected the single-character reference to resolve, got %v", resolveErr)
    }

    if "short" != escapedValue {
        t.Fatalf("expected the referenced parameter to resolve, got %q", escapedValue)
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
    projectDirectoryParameter := newParameter("", "/srv/app%1", "/srv/app%1", true)

    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            KernelProjectDirectory:  projectDirectoryParameter,
            "kernel.logs_directory": newParameter("", "%kernel.project_directory%/var/log", "", true),
        },
    }

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the reference to resolve, got %v", resolveErr)
    }

    if "/srv/app%1/var/log" != configuration.getInternalParameter("kernel.logs_directory").String() {
        t.Fatalf("unexpected logs dir %q", configuration.getInternalParameter("kernel.logs_directory").String())
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
        ConfigurationConfig{},
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the configuration to build, got %v", newConfigurationErr)
    }

    configuration.RegisterRuntime("app.tag", "%env(APP_TAG)%")

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("expected the pre-serving resolve to succeed, got %v", resolveErr)
    }

    configuration.MarkServing(configseam.NewToken())

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
        ConfigurationConfig{},
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the configuration to build, got %v", newConfigurationErr)
    }

    if resolveErr := configuration.Resolve(); nil != resolveErr {
        t.Fatalf("expected the pre-serving resolve to succeed, got %v", resolveErr)
    }

    configuration.MarkServing(configseam.NewToken())

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
            "app.name": newParameter("", "melody", "melody", false),
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

    if 8 != contextOfError(t, resolveErr)["offset"] {
        t.Fatalf("expected the percent that opened the name-shaped run to be the refused one, got offset %v", contextOfError(t, resolveErr)["offset"])
    }
    if _, carried := contextOfError(t, resolveErr)["reference"]; true == carried {
        t.Fatalf("expected no slice of the value in the context, got reference %v", contextOfError(t, resolveErr)["reference"])
    }
}

func TestResolveTemplate_UnclosedReferenceInsideACredentialStaysOutOfTheContext(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "Pa%SSword1",
        "database.password",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the unclosed reference to be refused")
    }

    renderedLogContext := fmt.Sprintf("%v", exception.LogContext(resolveErr, exception.LogContextOptions{}))
    if true == strings.Contains(renderedLogContext, "SSword1") {
        t.Fatalf("expected the credential's tail to stay out of the rendered log context: %s", renderedLogContext)
    }
    if 2 != contextOfError(t, resolveErr)["offset"] {
        t.Fatalf("expected the offset of the percent, got %v", contextOfError(t, resolveErr)["offset"])
    }
}

func TestResolveTemplate_UnterminatedEnvPlaceholderTailStaysOutOfTheContext(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    for _, template := range []string{"x%env(hunter2", "x%env(hunter2@host"} {
        _, resolveErr := configuration.resolveTemplate(
            template,
            "database.password",
            make(map[string]bool),
            make(map[string]bool),
        )
        if nil == resolveErr {
            t.Fatalf("%q: expected the unterminated placeholder to be refused", template)
        }

        renderedLogContext := fmt.Sprintf("%v", exception.LogContext(resolveErr, exception.LogContextOptions{}))
        if true == strings.Contains(renderedLogContext, "hunter2") {
            t.Fatalf("%q: expected the tail to stay out of the rendered log context: %s", template, renderedLogContext)
        }
        if 1 != contextOfError(t, resolveErr)["offset"] {
            t.Fatalf("%q: expected the offset of the percent, got %v", template, contextOfError(t, resolveErr)["offset"])
        }
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
            "app.signing_key": newParameter("", []byte(secretBytes), []byte(secretBytes), false),
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
                "zulu.broken":  newParameter("ZULU_BROKEN", "%missing.one%", nil, false),
                "alpha.broken": newParameter("ALPHA_BROKEN", "%missing.two%", nil, false),
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

        if "alpha.broken" != melodyErr.Context()["parameterName"] {
            t.Fatalf("iteration %d: expected the failure to name the sorted-first parameter, got %v", iteration, melodyErr.Context()["parameterName"])
        }
    }
}

func TestResolve_TheFailureNamesTheEnvironmentKeyBesideTheInternalAlias(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            "MELODY_LOG_PATH": "%app.name%/application.log",
        },
    }

    environment, environmentErr := NewEnvironment(context.Background(), source)
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    _, configurationErr := NewConfiguration(environment, t.TempDir(), ConfigurationConfig{})
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

    if KernelLogPath != resolutionContext["parameterName"] {
        t.Fatalf("expected the internal alias kept, got %v", resolutionContext["parameterName"])
    }

    if "MELODY_LOG_PATH" != resolutionContext["environmentKey"] {
        t.Fatalf("expected the key the operator actually wrote, got %v", resolutionContext["environmentKey"])
    }
}

func TestResolveTemplate_ARefusalInsideAnEnvironmentValueNamesTheEnvironmentKey(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{
            "DB_PASSWORD": "Pa%SSword1",
            "DB_OUTER":    "outer-%env(DB_PASSWORD)%",
        }},
        parameters: ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "pg://u:%env(DB_PASSWORD)%@h",
        "app.dsn",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the unclosed reference inside the environment value to be refused")
    }

    refusalContext := contextOfError(t, resolveErr)
    if "app.dsn" != refusalContext["parameterName"] || 2 != refusalContext["offset"] || "DB_PASSWORD" != refusalContext["environmentKey"] {
        t.Fatalf("expected the reader, the offset into the environment value and the environment key, got %v", refusalContext)
    }

    if true == strings.Contains(fmt.Sprintf("%v", exception.LogContext(resolveErr, exception.LogContextOptions{})), "SSword1") {
        t.Fatalf("expected the credential's tail to stay out of the rendered log context")
    }

    _, nestedErr := configuration.resolveTemplate(
        "%env(DB_OUTER)%",
        "app.nested",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == nestedErr {
        t.Fatalf("expected the nested environment value to be refused")
    }

    if "DB_PASSWORD" != contextOfError(t, nestedErr)["environmentKey"] {
        t.Fatalf("expected the innermost environment key, the one whose value the offset indexes, got %v", contextOfError(t, nestedErr))
    }
}

func TestResolveTemplate_ARefusalInsideAParameterReferencedFromAnEnvironmentValueNamesNoEnvironmentKey(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{
            "REFS_PARAM": "x-%app.inner%",
        }},
        parameters: ParameterMap{
            "app.inner": newParameter("APP_INNER", "Pa%SSword", nil, false),
        },
    }

    _, resolveErr := configuration.resolveTemplate(
        "%env(REFS_PARAM)%",
        "app.reader",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the unclosed reference inside the referenced parameter to be refused")
    }

    refusalContext := contextOfError(t, resolveErr)
    if "app.inner" != refusalContext["parameterName"] || 2 != refusalContext["offset"] {
        t.Fatalf("expected the referenced parameter and the offset into its value, got %v", refusalContext)
    }

    if _, named := refusalContext["environmentKey"]; true == named {
        t.Fatalf("expected no environment key on a refusal whose offset indexes a parameter's value, got %v", refusalContext)
    }
}

func newConfigurationFromEnvironmentValues(t *testing.T, values map[string]string) error {
    t.Helper()

    environment, environmentErr := NewEnvironment(context.Background(), &testEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    _, configurationErr := NewConfiguration(environment, t.TempDir(), ConfigurationConfig{})

    return configurationErr
}

func greetingKey() []configcontract.EnvironmentKey {
    return []configcontract.EnvironmentKey{applicationKey("APP_GREETING", "app.greeting", "")}
}

func TestNewConfiguration_RefusesAMalformedTemplateAndAnUndefinedEnvironmentReferenceAtConstruction(t *testing.T) {
    for name, value := range map[string]string{
        "malformed template":              "%env(APP_UNSET_KEY",
        "undefined environment reference": "%env(APP_UNSET_KEY)%",
    } {
        if _, configurationErr := newConfigurationWithApplicationKeys(map[string]string{"APP_GREETING": value}, greetingKey()); nil == configurationErr {
            t.Fatalf("%s: expected the construction refused", name)
        }
    }

    if _, configurationErr := newConfigurationWithApplicationKeys(map[string]string{"APP_GREETING": "%app.registered.later%"}, greetingKey()); nil != configurationErr {
        t.Fatalf("expected a reference to a parameter registered later deferred, got %v", configurationErr)
    }
}

func TestResolveTemplate_AnUndefinedEnvironmentKeyNamesTheReadingParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    _, resolveErr := configuration.resolveTemplate(
        "%env(MISSING_KEY_FOR_THE_PIN)%",
        "app.inner",
        make(map[string]bool),
        make(map[string]bool),
    )
    if nil == resolveErr {
        t.Fatalf("expected the undefined environment key to be refused")
    }

    refusalContext := contextOfError(t, resolveErr)
    if "app.inner" != refusalContext["parameterName"] || "MISSING_KEY_FOR_THE_PIN" != refusalContext["environmentKey"] {
        t.Fatalf("expected the reading parameter beside the environment key, got %v", refusalContext)
    }
}

func switchKey(defaultText string) configcontract.EnvironmentKey {
    return configcontract.EnvironmentKey{
        Key:       "MELODY_X_SWITCH",
        Parameter: "melody.x.switch",
        Field:     "x.ModuleConfig.Switch",
        Default:   defaultText,
        Rule:      SwitchKeyRule(),
    }
}

func resolvedReaderOfEnvironmentKey(t *testing.T, values map[string]string, key *configcontract.EnvironmentKey, environmentKey string) string {
    t.Helper()

    configuration := mustConfigurationFromValues(t, values)
    configuration.RegisterRuntime("app.reader", "%env("+environmentKey+")%")

    if nil != key {
        mustDeclare(t, configuration, "x", *key)
    }

    mustResolve(t, configuration)

    return configuration.Get("app.reader").String()
}

func TestEnvPlaceholder_ReadsTheDeclaredDefault_WhenNoArtifactSetsTheKey(t *testing.T) {
    if ":8080" != resolvedReaderOfEnvironmentKey(t, map[string]string{}, nil, HttpAddressKey) {
        t.Fatalf("expected the framework key's declared default")
    }

    moduleKey := textKey("fallback")
    if "fallback" != resolvedReaderOfEnvironmentKey(t, map[string]string{}, &moduleKey, moduleKey.Key) {
        t.Fatalf("expected the module key's declared default")
    }
}

func TestEnvPlaceholder_ArtifactLineWinsOverTheDeclaredDefault(t *testing.T) {
    moduleKey := textKey("fallback")
    if "from env" != resolvedReaderOfEnvironmentKey(t, map[string]string{moduleKey.Key: "from env"}, &moduleKey, moduleKey.Key) {
        t.Fatalf("expected the artifact's line over the module default")
    }

    if ":9090" != resolvedReaderOfEnvironmentKey(t, map[string]string{HttpAddressKey: ":9090"}, nil, HttpAddressKey) {
        t.Fatalf("expected the artifact's line over the framework default")
    }
}

func TestEnvPlaceholder_BlankLineUnderARuleTakesTheDefault(t *testing.T) {
    moduleKey := switchKey("true")
    if "true" != resolvedReaderOfEnvironmentKey(t, map[string]string{moduleKey.Key: ""}, &moduleKey, moduleKey.Key) {
        t.Fatalf("expected a blank line under a rule to take the declared default")
    }
}

func TestEnvPlaceholder_BlankLineWithoutARuleIsTheValue(t *testing.T) {
    moduleKey := textKey("fallback")
    if "" != resolvedReaderOfEnvironmentKey(t, map[string]string{moduleKey.Key: ""}, &moduleKey, moduleKey.Key) {
        t.Fatalf("expected a blank line without a rule to be the empty value")
    }
}

func TestEnvPlaceholder_UndeclaredAbsentKeyIsRefused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    configuration.RegisterRuntime("app.reader", "%env(APP_ABSENT)%")

    resolveErr := configuration.Resolve()

    for currentErr := resolveErr; nil != currentErr; currentErr = errors.Unwrap(currentErr) {
        refusal, isRefusal := currentErr.(*exception.Error)
        if true == isRefusal && "undefined environment key in template" == refusal.Message() {
            if "APP_ABSENT" != refusal.Context()["environmentKey"] || "app.reader" != refusal.Context()["parameterName"] {
                t.Fatalf("expected the key and the reader named, got %v", refusal.Context())
            }

            return
        }
    }

    t.Fatalf("expected an undefined environment key refusal, got %v", resolveErr)
}

func TestTemplatedDefault_IsScanned(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    configuration.RegisterRuntime("app.reader", "%env("+LogPathKey+")%")
    mustResolve(t, configuration)

    expected := configuration.Get(KernelLogsDirectory).String() + "/" + configuration.Get(KernelEnvironment).String() + ".log"
    if expected != configuration.Get("app.reader").String() {
        t.Fatalf("expected %q, got %q", expected, configuration.Get("app.reader").String())
    }
}

func TestKernelParameter_TheBootResolveAgreesWithTheView(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{CliNameKey: "n-%env(APP_X)%", "APP_X": "x"})
    mustResolve(t, configuration)

    if configuration.Cli().Name() != configuration.Get(KernelCliName).String() {
        t.Fatalf("expected the view and the parameter to agree, got %q and %q", configuration.Cli().Name(), configuration.Get(KernelCliName).String())
    }
}

func keyAsParameterRefusal(t *testing.T, err error) *exception.Error {
    t.Helper()

    for currentErr := err; nil != currentErr; currentErr = errors.Unwrap(currentErr) {
        refusal, isRefusal := currentErr.(*exception.Error)
        if true == isRefusal && "a template references an environment key as a parameter; a key is read as %env(KEY)%" == refusal.Message() {
            if false == errors.Is(refusal, errUndefinedParameterReference) {
                t.Fatalf("expected the deferrable cause, got %v", refusal)
            }

            return refusal
        }
    }

    t.Fatalf("expected the key-as-parameter refusal, got %v", err)

    return nil
}

func TestTemplate_KeyAsParameterIsRefused_NamingEnvForm(t *testing.T) {
    for _, row := range []struct {
        name   string
        values map[string]string
        key    string
    }{
        {name: "framework key", values: map[string]string{}, key: HttpAddressKey},
        {name: "loaded undeclared key", values: map[string]string{"APP_X": "x"}, key: "APP_X"},
    } {
        t.Run(row.name, func(t *testing.T) {
            configuration := mustConfigurationFromValues(t, row.values)
            configuration.RegisterRuntime("app.reader", "a-%"+row.key+"%")

            refusal := keyAsParameterRefusal(t, configuration.Resolve())
            context := refusal.Context()
            if "app.reader" != context["parameterName"] || row.key != context["parameterKey"] || 2 != context["offset"] {
                t.Fatalf("expected the reader, the key and the offset named, got %v", context)
            }
        })
    }
}

func TestTemplate_AParameterRegisteredLaterStillDefers(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})
    mustDeclare(t, configuration, "x", textKey("%app.later%"))
    configuration.RegisterRuntime("app.later", "late")
    mustResolve(t, configuration)

    if "late" != configuration.Get("melody.x.text").String() {
        t.Fatalf("expected the forward reference settled, got %q", configuration.Get("melody.x.text").String())
    }
}

func TestEnvPlaceholder_DefaultProcessorIsRefused_NamingTheDeclaration(t *testing.T) {
    for _, template := range []string{"%env(default::" + HttpAddressKey + ")%", "%env(default:kernel.environment:" + HttpAddressKey + ")%"} {
        configuration := mustConfigurationFromValues(t, map[string]string{})
        configuration.RegisterRuntime("app.reader", template)

        resolveErr := configuration.Resolve()

        found := false
        for currentErr := resolveErr; nil != currentErr; currentErr = errors.Unwrap(currentErr) {
            refusal, isRefusal := currentErr.(*exception.Error)
            if true == isRefusal && true == strings.HasPrefix(refusal.Message(), "malformed environment placeholder in template; the only form is %env(KEY)%; a key's fallback is the Default of its EnvironmentKey declaration") {
                found = true
            }
        }

        if false == found {
            t.Fatalf("expected the reworded malformed refusal for %q, got %v", template, resolveErr)
        }
    }
}

func TestEnvPlaceholder_ADottedLineIsReadable_AndItsParameterSpellingIsRefused(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"db.host": "h"})
    configuration.RegisterRuntime("app.reader", "x-%env(db.host)%")
    mustResolve(t, configuration)

    if "x-h" != configuration.Get("app.reader").String() {
        t.Fatalf("expected the dotted line read, got %q", configuration.Get("app.reader").String())
    }

    spelled := mustConfigurationFromValues(t, map[string]string{"db.host": "h"})
    spelled.RegisterRuntime("app.reader", "x-%db.host%")
    keyAsParameterRefusal(t, spelled.Resolve())
}

func TestTemplate_KeyAsParameterInsideALineNamesTheLine(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{"APP_PASS": "p", "APP_DSN": "mysql://u:%APP_PASS%@h"})
    configuration.RegisterRuntime("app.reader", "prefix %env(APP_DSN)%")

    context := keyAsParameterRefusal(t, configuration.Resolve()).Context()
    if "APP_DSN" != context["environmentKey"] || "app.reader" != context["parameterName"] || 10 != context["offset"] {
        t.Fatalf("expected the line holding the reference named, got %v", context)
    }
}
