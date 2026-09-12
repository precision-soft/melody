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
        /* a bare integer is refused for the same missing unit as the bare number string — it used to be read as nanoseconds, a timeout that fired instantly with no error anywhere */
        {"bareInt", int(5)},
        {"bareInt64", int64(5)},
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

/* parameters routinely hold inline credentials, so a failed conversion must identify the parameter by its environment key alone; embedding the offending value would carry the secret into logs through the exception cause-context chain */
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

/* Resolve rewrites every parameter's value, while a service handed the *Parameter reads it through the accessors without ever touching the configuration. The write was covered by the configuration lock and the read by nothing, which is two locks around one field — the race detector reported the write at the resolve loop against the read in Value(). */
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

    /* a plain value, not a template: a pre-boot templated registration is deferred and refuses to be read until boot, so it cannot exercise the value/valueMutex race this test measures — Resolve still rewrites this parameter's value under the write lock, which is the write side the read races. */
    configuration.RegisterRuntime("app.tag", "tag")

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

/* a secret parameter's conversion failure withholds the cause: the strconv text quotes the value it refused, which is the right diagnostic for a pool size and the wrong log line for a credential; an ordinary parameter keeps the full cause */
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

/* a deferred parameter still holds its raw template, and every accessor funnels through loadValue: the read refuses loudly instead of serving %app.user% as though it were the value, and the refusal names the parameter the way every diagnostic here does */
func TestParameter_ReadingADeferredParameterRefusesLoudly(t *testing.T) {
    parameter := NewParameter("APP_GREETING", "%app.user%", "%app.user%", false)
    parameter.deferred.Store(true)

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the accessor to refuse a deferred parameter")
        }

        recoveredErr, isException := recoveredValue.(*exception.Error)
        if false == isException {
            t.Fatalf("expected an exception error, got %T", recoveredValue)
        }

        if false == strings.Contains(recoveredErr.Error(), "deferred to boot") {
            t.Fatalf("unexpected refusal message: %q", recoveredErr.Error())
        }

        if "APP_GREETING" != recoveredErr.Context()["environmentKey"] {
            t.Fatalf("expected the refusal to name the environment key, got %#v", recoveredErr.Context()["environmentKey"])
        }
    }()

    _ = parameter.String()
}

/* the settled side of the deferral: once the boot resolution stores a value and clears the flag, the accessors answer as they always did */
func TestParameter_ASettledDeferredParameterReadsNormally(t *testing.T) {
    parameter := NewParameter("APP_GREETING", "%app.user%", "%app.user%", false)
    parameter.deferred.Store(true)

    parameter.storeValue("hello operator")
    parameter.deferred.Store(false)

    if "hello operator" != parameter.String() {
        t.Fatalf("unexpected value %q", parameter.String())
    }
}

/* MustString is the accessor wiring code reaches for when a missing string is a boot failure, so its refusal has to name what it refused: the environment key, the registration name of a runtime parameter that has no key, and the type it actually found — the value itself stays out, the way every other conversion diagnostic here does. */
func TestParameter_MustString_PanicRefusalNamesTheParameterAndTheTypeItFound(t *testing.T) {
    parameter := NewParameter("APP_POOL_SIZE", 12, 12, false)
    parameter.name = "app.pool.size"

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected MustString to panic for a non-string value")
        }

        recoveredErr, isException := recoveredValue.(*exception.Error)
        if false == isException {
            t.Fatalf("expected an exception error, got %T", recoveredValue)
        }

        if "cannot convert parameter value to string" != recoveredErr.Error() {
            t.Fatalf("unexpected refusal message: %q", recoveredErr.Error())
        }

        context := recoveredErr.Context()

        if "APP_POOL_SIZE" != context["environmentKey"] {
            t.Fatalf("expected the refusal to name the environment key, got %#v", context["environmentKey"])
        }

        if "app.pool.size" != context["parameterName"] {
            t.Fatalf("expected the refusal to name the registration name, got %#v", context["parameterName"])
        }

        if "int" != context["valueType"] {
            t.Fatalf("expected the refusal to name the type it found, got %#v", context["valueType"])
        }

        if true == strings.Contains(fmt.Sprintf("%v", context), "12") {
            t.Fatalf("expected the refused value itself to stay out of the context, got %#v", context)
        }
    }()

    _ = parameter.MustString()
}

/* a parameter that really holds a string is handed back by MustString without any refusal — the panic above is the refusal path, not the ordinary one */
func TestParameter_MustString_ReturnsTheStoredString(t *testing.T) {
    parameter := NewParameter("APP_NAME", "melody", "melody", false)

    if "melody" != parameter.MustString() {
        t.Fatalf("expected the stored string, got %q", parameter.MustString())
    }
}

/* a runtime parameter has no environment key, and passing the empty key into the shared parsers put a nameless parameterName inside the cause of an error whose outer context names the parameter — the operator reading the cause chain concluded the parameter was anonymous */
func TestParameter_ConversionCauseNamesTheRuntimeParameter(t *testing.T) {
    parameter := NewParameter("", "not-a-duration", "not-a-duration", false)
    parameter.name = "app.timeout"

    _, durationErr := parameter.Duration()
    if nil == durationErr {
        t.Fatalf("expected the conversion to fail")
    }

    var causeErr *exception.Error
    outerErr, isOuter := durationErr.(*exception.Error)
    if false == isOuter {
        t.Fatalf("expected an exception error, got %T", durationErr)
    }

    if false == errors.As(outerErr.Unwrap(), &causeErr) {
        t.Fatalf("expected an exception cause, got %T", outerErr.Unwrap())
    }

    if "app.timeout" != causeErr.Context()["parameterName"] {
        t.Fatalf("expected the cause to name the runtime parameter, got %#v", causeErr.Context()["parameterName"])
    }
}

/* Bool reads a native bool as itself and a string through the shared parser, and refuses everything else by name: a parameter that decides whether a feature is on must never answer false because it happened to hold a number, and the value of a secret never reaches the diagnostic */
func TestParameter_Bool_ReadsBothShapesAndRefusesTheRest(t *testing.T) {
    nativeParameter := NewParameter("APP_FEATURE", true, true, false)

    nativeValue, nativeErr := nativeParameter.Bool()
    if nil != nativeErr {
        t.Fatalf("unexpected error: %v", nativeErr)
    }
    if false == nativeValue {
        t.Fatalf("expected the native bool to be handed back")
    }

    for _, textualValue := range []string{"true", "1", "false", "0"} {
        textualParameter := NewParameter("APP_FEATURE", textualValue, textualValue, false)

        parsedValue, parsedErr := textualParameter.Bool()
        if nil != parsedErr {
            t.Fatalf("unexpected error for %q: %v", textualValue, parsedErr)
        }

        expected := "true" == textualValue || "1" == textualValue
        if expected != parsedValue {
            t.Fatalf("expected %q to read as %v, got %v", textualValue, expected, parsedValue)
        }
    }

    unparsableParameter := NewParameter("APP_FEATURE", "yes-please", "yes-please", false)
    _, unparsableErr := unparsableParameter.Bool()
    if nil == unparsableErr || "cannot convert parameter value to bool" != unparsableErr.Error() {
        t.Fatalf("expected the conversion refusal, got %v", unparsableErr)
    }

    foreignParameter := NewParameter("APP_FEATURE", 12, 12, false)
    _, foreignErr := foreignParameter.Bool()
    if nil == foreignErr || "cannot convert parameter value to bool" != foreignErr.Error() {
        t.Fatalf("expected a value of another type to be refused, got %v", foreignErr)
    }

    var foreignExceptionErr *exception.Error
    if false == errors.As(foreignErr, &foreignExceptionErr) {
        t.Fatalf("expected an exception error")
    }

    /* the cause is the parser's, and it names what the refusal is about: the outer message says only that a conversion failed, which told an operator holding a parameter registered as a number that something went wrong and nothing about what */
    foreignCause := foreignExceptionErr.CauseErr()
    if nil == foreignCause {
        t.Fatalf("expected the shared parser's cause to be carried")
    }

    var foreignCauseErr *exception.Error
    if false == errors.As(foreignCause, &foreignCauseErr) {
        t.Fatalf("expected the cause to be an exception error, got %v", foreignCause)
    }

    causeContext := foreignCauseErr.Context()
    if "APP_FEATURE" != causeContext["parameterName"] {
        t.Fatalf("expected the cause to name the parameter, got %#v", causeContext["parameterName"])
    }
    if "bool" != causeContext["expectedType"] {
        t.Fatalf("expected the cause to name the target type, got %#v", causeContext["expectedType"])
    }
    if nil == causeContext["value"] {
        t.Fatalf("expected the cause to carry the value it refused, got %#v", causeContext)
    }
}

/* a secret keeps its value out of the diagnostic on this door too: the shared parser stamps the value it refused into the cause context, and delegating to it would have published a secret's contents through the very chain that exists to explain the refusal */
func TestParameter_Bool_KeepsASecretValueOutOfTheCause(t *testing.T) {
    secretParameter := NewParameter("APP_SECRET", 12, 12, false)
    secretParameter.isSecret.Store(true)

    _, secretErr := secretParameter.Bool()
    if nil == secretErr {
        t.Fatalf("expected the refusal")
    }

    var secretExceptionErr *exception.Error
    if false == errors.As(secretErr, &secretExceptionErr) {
        t.Fatalf("expected an exception error")
    }

    if nil != secretExceptionErr.CauseErr() {
        t.Fatalf("expected a secret's parser cause to be withheld, got %v", secretExceptionErr.CauseErr())
    }
}

func TestParameter_IntReadsTheSameGrammarItsSiblingsRead(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, environmentErr := NewEnvironment(source)
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    configuration, configurationErr := NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("new configuration error: %v", configurationErr)
    }

    configuration.RegisterRuntime("app.batch_size", int64(500))

    intValue, intErr := configuration.MustGet("app.batch_size").Int()
    if nil != intErr {
        t.Fatalf("expected the int64 a sibling accessor already converts, got: %v", intErr)
    }

    if 500 != intValue {
        t.Fatalf("expected 500, got %d", intValue)
    }

    configuration.RegisterRuntime("app.retry_count", "  7 ")

    stringSourced, stringSourcedErr := configuration.MustGet("app.retry_count").Int()
    if nil != stringSourcedErr || 7 != stringSourced {
        t.Fatalf("expected the string spelling to keep converting, got %d and %v", stringSourced, stringSourcedErr)
    }

    configuration.RegisterRuntime("app.not_a_number", "seven")

    if _, refusalErr := configuration.MustGet("app.not_a_number").Int(); nil == refusalErr {
        t.Fatal("expected a value that is not a number to stay refused")
    }
}
