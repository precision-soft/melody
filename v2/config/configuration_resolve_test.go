package config

import (
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/exception"
)

/* @info a self-referential env value expands to itself plus extra characters on every pass; the env-substitution branch is not covered by the parameter-recursion cycle guard, so without the pass bound the fixed-point loop never terminates */
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

/* @info an undefined env placeholder must be reported by key only; the raw parameter value routinely carries inline credentials that would otherwise reach logs through the exception cause-context chain */
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

/* @info an undefined parameter placeholder must be reported by key only, never with the raw value that may embed credentials */
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

/* @info a single colon is the most likely misspelling of the default processor; the strict pattern cannot match it, so without the shape guard it would survive resolution as literal text and reach the service as the string "%env(default:KEY)%" */
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

/* @info the default processor must stay opt-in: a plain placeholder that silently degraded to an empty string would boot the application with an unset credential instead of refusing to start */
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

/* @info the fallback is handed back as a parameter placeholder, so an undefined fallback must surface the parameter branch error rather than resolve to the literal text */
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

/* @info a value holding a literal percent — a generated password is the common case — is written with the percent doubled; without the escape it reads as a parameter reference and the boot fails, which is what the resolution and validation messages have to point at */
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

func TestConfiguration_UnescapedLiteralPercentFailsWithAnActionableMessage(t *testing.T) {
    _, newConfigurationErr := NewConfiguration(
        &Environment{
            values: map[string]string{
                "DATABASE_PASSWORD": "pa%ss%word",
            },
        },
        "/srv/app",
    )
    if nil == newConfigurationErr {
        t.Fatalf("expected the unescaped value to fail the boot")
    }

    /* the boot error wraps the resolution failure, and Error() reports only its own message, so the actionable one is found by walking the cause chain */
    messages := ""
    for err := newConfigurationErr; nil != err; err = errors.Unwrap(err) {
        messages = messages + err.Error() + "\n"
    }

    if false == strings.Contains(messages, "%%") {
        t.Fatalf("expected the cause chain to point at the escape, got %s", messages)
    }
}

/* @info an environment value is a template of its own: its doubled percents are its literals and must survive the splice as data instead of being rescanned as the parameter reference %ss% */
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

/* @info a referenced parameter's resolved value is data: a password holding a literal percent must splice into the dsn that reads it without being rescanned as a reference */
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

/* @info marking the env-registered parameter is how a credential read through %env(KEY)% is declared, and the marking must travel to the reader exactly as it does on the parameter branch */
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

/* @info an env value carrying a single-percent malformed shape still fails: values are templates by design, and the doubled-percent escape is the supported way to write a literal percent */
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

/* @info a self-reference is a cycle like any other: the scanner reports it at resolve time instead of leaving the placeholder behind as literal text for an after-the-fact check to catch */
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

/* @info a literal percent written with the doubled escape survives a splice as data, and adjacent references resolve instead of swallowing each other */
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

    /* the adjacency %app.a%%app.b% resolves both references, where an escape-first reading would have swallowed the touching percents */
    if "x_pa%ss%wordpa%ss%word" != configuration.getInternalParameter("app.c").String() {
        t.Fatalf("unexpected adjacent resolution %q", configuration.getInternalParameter("app.c").String())
    }
}

/* @info the default processor accepts a single-character fallback name, so the parameter pattern has to resolve it too instead of leaving %a% as literal text */
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

/* @info the candidate ends where the placeholder grammar ends: a literal %env( must stay data even when a different, well-formed placeholder closes later in the same value */
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

/* @info the project directory is a filesystem path, not a template: a literal percent in it survives a reference and the parameter itself is never overwritten */
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

/* @info a misspelled placeholder that still closes (%env(FOO-BAR)%) is reported, while a literal "%env(" whose closer belongs to a different placeholder stays data — the candidate ends at the first ")%" no percent interrupts */
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
