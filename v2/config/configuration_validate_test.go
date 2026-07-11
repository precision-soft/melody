package config

import (
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/exception"
)

/* @info the unresolved-placeholder validation must report only the parameter name and the offending placeholder identifier; the resolved value routinely carries inline credentials that would otherwise reach logs through the exception cause-context chain */
func TestValidateNoUnresolvedPlaceholders_ErrorOmitsResolvedValue(t *testing.T) {
    resolvedValue := "mysql://root:S3cretPassword@tcp(db:3306)/app?tls=%env(DB_TLS)%"

    configuration := &Configuration{
        parameters: ParameterMap{
            "database.url": NewParameter("DATABASE_URL", resolvedValue, resolvedValue, false),
        },
    }

    err := configuration.validateNoUnresolvedPlaceholders()
    if nil == err {
        t.Fatalf("expected an unresolved placeholder error")
    }

    exceptionError, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected an *exception.Error, got %T", err)
    }

    context := exceptionError.Context()

    if _, present := context["value"]; true == present {
        t.Fatalf("error context must not embed the resolved parameter value")
    }

    if true == strings.Contains(fmt.Sprintf("%v", context), "S3cretPassword") {
        t.Fatalf("error context leaked the inline credential: %v", context)
    }

    if "database.url" != context["parameterName"] {
        t.Fatalf("expected the parameter name in the context, got %v", context["parameterName"])
    }

    if "%env(DB_TLS)%" != context["placeholder"] {
        t.Fatalf("expected the offending placeholder in the context, got %v", context["placeholder"])
    }
}
