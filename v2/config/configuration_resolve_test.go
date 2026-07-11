package config

import (
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/exception"
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
