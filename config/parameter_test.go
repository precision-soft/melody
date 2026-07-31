package config

import (
    "errors"
    "fmt"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/exception"
)

func TestParameter_Duration_ParsesStringAndNativeValues(t *testing.T) {
    cases := []struct {
        name     string
        value    any
        expected time.Duration
    }{
        {"string", "1500ms", 1500 * time.Millisecond},
        {"stringWithSurroundingSpace", "  2s  ", 2 * time.Second},
        {"native", 3 * time.Second, 3 * time.Second},
        {"int", int(5), time.Duration(5)},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            parameter := NewParameter("SESSION_TTL", testCase.value, testCase.value, false)

            durationValue, durationErr := parameter.Duration()
            if nil != durationErr {
                t.Fatalf("expected the value to convert, got %v", durationErr)
            }

            if testCase.expected != durationValue {
                t.Fatalf("expected %v, got %v", testCase.expected, durationValue)
            }
        })
    }
}

func TestParameter_Duration_RejectsUnparsableAndUnsetValues(t *testing.T) {
    cases := []struct {
        name  string
        value any
    }{
        {"unparsableString", "not-a-duration"},
        {"bareNumberString", "30"},
        {"unset", nil},
        {"unsupportedType", true},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            parameter := NewParameter("SESSION_TTL", testCase.value, testCase.value, false)

            _, durationErr := parameter.Duration()
            if nil == durationErr {
                t.Fatalf("expected a conversion error for %v", testCase.value)
            }
        })
    }
}

func TestParameter_Float_ParsesStringAndNativeValues(t *testing.T) {
    cases := []struct {
        name     string
        value    any
        expected float64
    }{
        {"string", "1.5", 1.5},
        {"stringWithSurroundingSpace", " 2.25 ", 2.25},
        {"native", float64(3.5), 3.5},
        {"int", int(4), 4},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            parameter := NewParameter("SAMPLE_RATE", testCase.value, testCase.value, false)

            floatValue, floatErr := parameter.Float()
            if nil != floatErr {
                t.Fatalf("expected the value to convert, got %v", floatErr)
            }

            if testCase.expected != floatValue {
                t.Fatalf("expected %v, got %v", testCase.expected, floatValue)
            }
        })
    }
}

func TestParameter_Float_RejectsUnparsableAndUnsetValues(t *testing.T) {
    cases := []struct {
        name  string
        value any
    }{
        {"unparsableString", "not-a-float"},
        {"unset", nil},
        {"unsupportedType", true},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            parameter := NewParameter("SAMPLE_RATE", testCase.value, testCase.value, false)

            _, floatErr := parameter.Float()
            if nil == floatErr {
                t.Fatalf("expected a conversion error for %v", testCase.value)
            }
        })
    }
}

/* @info parameters routinely hold inline credentials, so a failed conversion must identify the parameter by its environment key alone; embedding the offending value would carry the secret into logs through the exception cause-context chain */
func TestParameter_ConversionErrorsOmitTheRawValue(t *testing.T) {
    secretValue := "P4ssPhrase"

    parameter := NewParameter("AIRBNB_CLIENT_SECRET", secretValue, secretValue, false)

    _, durationErr := parameter.Duration()
    if nil == durationErr {
        t.Fatalf("expected a conversion error")
    }

    _, floatErr := parameter.Float()
    if nil == floatErr {
        t.Fatalf("expected a conversion error")
    }

    for _, err := range []error{durationErr, floatErr} {
        context := contextOfError(t, err)

        assertContextOmitsSecret(t, context, secretValue)

        if "AIRBNB_CLIENT_SECRET" != context["environmentKey"] {
            t.Fatalf("expected the environment key in the context, got %v", context["environmentKey"])
        }

        if true == strings.Contains(fmt.Sprintf("%v", err), secretValue) {
            t.Fatalf("error message leaked the value: %v", err)
        }
    }
}

/* @info Resolve rewrites every parameter's value, while a service handed the *Parameter reads it through the accessors without ever touching the configuration. The write was covered by the configuration lock and the read by nothing, which is two locks around one field — the race detector reported the write at the resolve loop against the read in Value(). */
func TestParameter_ValueDoesNotRaceResolve(t *testing.T) {
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

    parameter := configuration.Get("app.tag")
    if nil == parameter {
        t.Fatalf("expected the registered parameter to exist")
    }

    var waitGroup sync.WaitGroup

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()

        for iteration := 0; iteration < 200; iteration = iteration + 1 {
            resolveErr := configuration.Resolve()
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %v", resolveErr)

                return
            }
        }
    }()

    waitGroup.Add(1)
    go func() {
        defer waitGroup.Done()

        for iteration := 0; iteration < 200; iteration = iteration + 1 {
            _ = parameter.Value()
            _ = parameter.String()
        }
    }()

    waitGroup.Wait()
}

/* @info a secret parameter's conversion failure withholds the cause: the strconv text quotes the value it refused, which is the right diagnostic for a pool size and the wrong log line for a credential; an ordinary parameter keeps the full cause */
func TestParameter_SecretConversionWithholdsTheValue(t *testing.T) {
    secretParameter := NewParameter("APP_TOKEN", "sk_live_51H", "sk_live_51H", false)
    secretParameter.isSecret.Store(true)

    _, intErr := secretParameter.Int()
    if nil == intErr {
        t.Fatalf("expected the conversion to fail")
    }
    if true == strings.Contains(intErr.Error(), "sk_live_51H") {
        t.Fatalf("expected the secret value to stay out of the error text")
    }

    var exceptionErr *exception.Error
    if false == errors.As(intErr, &exceptionErr) {
        t.Fatalf("expected an exception error")
    }
    if nil != exceptionErr.CauseErr() {
        t.Fatalf("expected the quoting cause to be withheld for a secret")
    }

    ordinaryParameter := NewParameter("APP_POOL", "1O0", "1O0", false)

    _, ordinaryErr := ordinaryParameter.Int()
    if nil == ordinaryErr {
        t.Fatalf("expected the conversion to fail")
    }
    if false == errors.As(ordinaryErr, &exceptionErr) || nil == exceptionErr.CauseErr() {
        t.Fatalf("expected the ordinary parameter to keep its diagnostic cause")
    }
}
