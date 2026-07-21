package config

import (
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/exception"
)

/* @info a self-referential env value expands to itself plus extra characters on every pass; the env-substitution branch is not covered by the parameter-recursion cycle guard, so without the pass bound the fixed-point loop never terminates */
func TestResolveWithTemplates_SelfReferentialEnvValueReportsCircularReference(t *testing.T) {
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
        value, err := configuration.resolveWithTemplates(
            "x%env(APP_A)%",
            "app.a",
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
        t.Fatalf("resolveWithTemplates never returned: the self-referential env value looped forever")
    }
}

/* @info an undefined env placeholder must be reported by key only; the raw parameter value routinely carries inline credentials that would otherwise reach logs through the exception cause-context chain */
func TestResolveWithTemplates_UndefinedEnvironmentKeyErrorOmitsRawValue(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    secretValue := "mysql://root:S3cretPassword@tcp(db:3306)/app?tls=%env(DB_TLS)%"

    _, err := configuration.resolveWithTemplates(secretValue, "database.url", make(map[string]bool))
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
func TestResolveWithTemplates_UndefinedParameterKeyErrorOmitsRawValue(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters:  ParameterMap{},
    }

    secretValue := "postgres://user:P4ssPhrase@host:5432/db?opt=%missing_parameter%"

    _, err := configuration.resolveWithTemplates(secretValue, "database.dsn", make(map[string]bool))
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
func TestResolveSinglePass_MalformedEnvPlaceholderIsReported(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    _, err := configuration.resolveWithTemplates(
        "%env(default:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
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

func TestResolveWithTemplates_DefaultProcessorYieldsEmptyStringForUndefinedKey(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    value, err := configuration.resolveWithTemplates(
        "%env(default::AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
    )
    if nil != err {
        t.Fatalf("expected the undefined key to be tolerated, got %v", err)
    }

    if "" != value {
        t.Fatalf("expected an empty value, got %q", value)
    }
}

func TestResolveWithTemplates_DefaultProcessorFallsBackToParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{
            "aws.default_endpoint": NewParameter("", "http://localstack:4566", "http://localstack:4566", false),
        },
    }

    value, err := configuration.resolveWithTemplates(
        "%env(default:aws.default_endpoint:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
        make(map[string]bool),
    )
    if nil != err {
        t.Fatalf("expected the fallback parameter to resolve, got %v", err)
    }

    if "http://localstack:4566" != value {
        t.Fatalf("expected the fallback parameter value, got %q", value)
    }
}

func TestResolveWithTemplates_DefaultProcessorPrefersDefinedEnvironmentValue(t *testing.T) {
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

    value, err := configuration.resolveWithTemplates(
        "%env(default:aws.default_endpoint:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
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
func TestResolveWithTemplates_PlainPlaceholderStillFailsForUndefinedKey(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    _, err := configuration.resolveWithTemplates(
        "%env(AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
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
func TestResolveWithTemplates_DefaultProcessorReportsUndefinedFallbackParameter(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{
            values: map[string]string{},
        },
        parameters: ParameterMap{},
    }

    _, err := configuration.resolveWithTemplates(
        "%env(default:aws.missing_fallback:AWS_ENDPOINT_URL)%",
        "aws.endpoint_url",
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
